package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/egress"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/secret"
)

// Places は sbxr が host 側に置くもの。
type Places struct {
	// StateRoot は状態ディレクトリの親 (${XDG_STATE_HOME:-~/.local/state}/sbxr/sandboxes。ADR 0006)。
	StateRoot string
	// CacheRoot は git URL の cache clone の親。
	CacheRoot string
	// UserConfig は user 設定の path。
	UserConfig string
}

// StateDir は sandbox VM の状態ディレクトリ。
func (p Places) StateDir(name string) string {
	return filepath.Join(p.StateRoot, name)
}

// RepoDeclarationFile は repo 宣言のファイル名。
const RepoDeclarationFile = "sbxr.yaml"

// 状態ディレクトリに sbxr が置くファイル。declaration.yaml は作成がすべて済んでから書き、作成が終わった印にする。
// 実行基盤の定義 (sbx では env 定義と kit) は Runtime が同じディレクトリに書く。
const (
	sourceFile      = "source"
	declarationFile = "declaration.yaml"
)

// Declaration は作成時に確定した宣言。状態ディレクトリに残し、drift 検出の基準にする。
type Declaration struct {
	Profile config.Profile `yaml:"profile"`
	Git     struct {
		Name  string `yaml:"name"`
		Email string `yaml:"email"`
	} `yaml:"git"`
	Init []string `yaml:"init"`
	Boot []string `yaml:"boot"`
	// SandboxEgress は sandbox スコープ rule にする宛先 (repo の egress)。
	SandboxEgress []string `yaml:"sandbox_egress"`
	// Secrets は配線する secret。形は配線の結果が決める。
	Secrets []secret.WiredSecret `yaml:"secrets"`
	// Herdr は herdr 連携。無効なら書かない。
	Herdr *HerdrPin `yaml:"herdr,omitempty"`
}

// HerdrPin は作成時に確定した herdr 連携 (VM に入れる版)。
type HerdrPin struct {
	Version string `yaml:"version"`
}

// Prepared は create の確認関門で見せ、承認後に作る内容。
type Prepared struct {
	Target      Target
	Declaration Declaration
	// GlobalEgress は全 sandbox VM に効く global rule の宛先 (sbxr policy sync が収束させる)。
	GlobalEgress []string
	// RepoEgress は repo の egress をどう扱って確定したか。作成時の記録に残し、drift を比べるときに同じ扱いを再現する。
	RepoEgress RepoEgressPolicy
	// DroppedRepoEgress は git URL を --yes で通したために落とした repo の egress の宛先。
	DroppedRepoEgress []string
	Wiring            secret.Plan
	VMEnv             map[string]string
	// OriginHost は repo の origin の host。VM の git で ssh 形をこの host の https へ書き換える。origin が無ければ空。
	OriginHost string
	// Warnings は作成を止めないが、利用者に見せる警告。
	Warnings []error
}

// RepoEgressPolicy は repo 宣言の egress を sandbox スコープ rule にするか。
type RepoEgressPolicy int

const (
	// KeepRepoEgress は repo の egress を sandbox スコープ rule にする。
	KeepRepoEgress RepoEgressPolicy = iota
	// DropRepoEgress は repo の egress を捨てる。人間が確認関門で見ていない untrusted な宣言から宛先を開けないため。
	DropRepoEgress
)

// Prepare は 3 スコープの宣言を merge して作る内容を確定する。git URL の Target は呼び出し側が clone してから渡す。
func Prepare(ctx context.Context, places Places, target Target, repoEgress RepoEgressPolicy) (Prepared, error) {
	if info, err := os.Stat(target.Repo); err != nil || !info.IsDir() {
		return Prepared{}, fmt.Errorf("repo のディレクトリ %s が無い", target.Repo)
	}
	host, warning := originHost(ctx, target.Repo)
	cfg, err := config.Load(places.UserConfig, filepath.Join(target.Repo, RepoDeclarationFile))
	if err != nil {
		return Prepared{}, err
	}
	globalGroups, err := egress.ParseGroups(cfg.GlobalEgress)
	if err != nil {
		return Prepared{}, err
	}
	sandboxGroups, err := egress.ParseGroups(cfg.SandboxEgress)
	if err != nil {
		return Prepared{}, err
	}
	prepared := Prepared{Target: target, RepoEgress: repoEgress, GlobalEgress: egress.DesiredResources(globalGroups), OriginHost: host}
	if warning != nil {
		prepared.Warnings = append(prepared.Warnings, warning)
	}
	sandboxEgress := egress.DesiredResources(sandboxGroups)
	if repoEgress == DropRepoEgress {
		prepared.DroppedRepoEgress, sandboxEgress = sandboxEgress, nil
	}
	defs, err := secret.ParseDefinitions(cfg.SecretDefs)
	if err != nil {
		return Prepared{}, err
	}
	prepared.Wiring, err = secret.PlanWiring(cfg.Secrets, defs, append(slices.Clone(prepared.GlobalEgress), sandboxEgress...))
	if err != nil {
		return Prepared{}, err
	}
	prepared.VMEnv, err = prepared.Wiring.VMEnv()
	if err != nil {
		return Prepared{}, err
	}

	decl := &prepared.Declaration
	decl.Profile = cfg.Profile
	decl.Git.Name, decl.Git.Email = cfg.Git.Name, cfg.Git.Email
	decl.Init, decl.Boot = cfg.Init, cfg.Boot
	decl.SandboxEgress = sandboxEgress
	if cfg.Herdr.Enabled {
		decl.Herdr = &HerdrPin{Version: cfg.Herdr.Version}
	}
	decl.Secrets = prepared.Wiring.WiredSecrets()
	return prepared, nil
}

// Situation は sandbox VM と sbxr の状態ディレクトリの組み合わせ。
type Situation int

const (
	// Absent は VM も状態ディレクトリも無い。
	Absent Situation = iota
	// Unmanaged は VM はあるが sbxr の状態ディレクトリが無い (sbxr が作っていない VM)。
	Unmanaged
	// OtherSource は同じ名前の状態ディレクトリが別の repo のもの。
	OtherSource
	// Incomplete は前回の create が途中で止まった (作成が終わった印が無い)。
	Incomplete
	// Ready は sbxr が作り終えた VM。
	Ready
	// Vanished は作成時の宣言があるのに、VM が sbxr の外で撤去されている (VM 消失)。destroy で片付ける。
	Vanished
)

// Inspection は Inspect の結果。
type Inspection struct {
	Situation Situation
	Status    runtime.SandboxStatus
	// recordedSource は状態ディレクトリに記録された出所。
	recordedSource string
}

// Inspect は target の sandbox VM と状態ディレクトリを調べる。
func Inspect(ctx context.Context, rt runtime.Runtime, places Places, target Target) (Inspection, error) {
	status, err := rt.SandboxStatus(ctx, target.Name)
	if err != nil {
		return Inspection{}, err
	}
	dir := places.StateDir(target.Name)
	recorded, found, err := recordedSource(dir)
	switch {
	case err != nil:
		return Inspection{}, err
	case !found && status == runtime.SandboxAbsent:
		return Inspection{Situation: Absent, Status: status}, nil
	case !found:
		return Inspection{Situation: Unmanaged, Status: status}, nil
	case recorded != target.Source():
		return Inspection{Situation: OtherSource, Status: status, recordedSource: recorded}, nil
	}
	if _, err := os.Stat(filepath.Join(dir, declarationFile)); errors.Is(err, fs.ErrNotExist) {
		return Inspection{Situation: Incomplete, Status: status}, nil
	} else if err != nil {
		return Inspection{}, err
	}
	if status == runtime.SandboxAbsent {
		return Inspection{Situation: Vanished, Status: status}, nil
	}
	return Inspection{Situation: Ready, Status: status}, nil
}

// RequireManaged は sbxr が作った (作りかけを含む) VM でなければ、理由を error で返す。
func (i Inspection) RequireManaged(name string) error {
	switch i.Situation {
	case Absent:
		return fmt.Errorf("sandbox VM %s は無い", name)
	case Unmanaged:
		return fmt.Errorf("sandbox VM %s は sbxr の管理外 (sbxr の状態ディレクトリが無い) なので触らない", name)
	case OtherSource:
		return fmt.Errorf("sandbox VM %s は別の repo (%s) から作られている", name, i.recordedSource)
	}
	return nil
}

// NotRunning は VM が止まっているか無いか (使用中でないと言えるか) を返す。
func (i Inspection) NotRunning() bool {
	return i.Status == runtime.SandboxStopped || i.Status == runtime.SandboxAbsent
}

// Create は作る内容を組み立てて実行基盤に定義と作成を頼み (secret と rule をどの順で置くかは実行基盤が守る。decision/0009)、
// VM の中を宣言どおりにし (materialize → read-back → init → boot)、VM 内から egress 自己検証を行う。
// 途中で失敗したら状態ディレクトリと VM を残す (destroy がそれを使って片付ける)。
// 作成が終わった印 (declaration.yaml) は最後に書く。VM を作れた後の段の失敗は *StageError で返す。
func Create(ctx context.Context, hosts Hosts, places Places, prepared Prepared, values secret.Values, progress io.Writer) error {
	rt := hosts.Runtime
	name := prepared.Target.Name
	stateDir := places.StateDir(name)
	spec, err := sandboxSpec(prepared, values)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("状態ディレクトリ %s を作れない: %w", stateDir, err)
	}
	// 定義を先に、出所を後に書く (出所だけが残ると、destroy が定義の無い状態ディレクトリで詰む。ADR 0006)
	if err := rt.DefineSandbox(stateDir, spec); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stateDir, sourceFile), []byte(prepared.Target.Source()), 0o600); err != nil {
		return fmt.Errorf("状態ディレクトリに %s を書けない: %w", sourceFile, err)
	}
	if err := rt.CreateSandbox(ctx, stateDir, spec); err != nil {
		return createdStageError(err)
	}
	if err := setUpInside(ctx, rt, prepared, progress); err != nil {
		return err
	}
	if err := writeRecord(stateDir, Record{Declaration: prepared.Declaration, RepoEgress: prepared.RepoEgress}); err != nil {
		return err
	}
	if prepared.Declaration.Herdr != nil {
		return registerHerdrMachine(ctx, hosts, name, progress)
	}
	return nil
}

// sandboxSpec は確定した宣言と secret の値から、実行基盤に渡す作る内容を組み立てる。
// secret の値を含むので、確認関門と plan が見せる Prepared には載せず、作る直前に組み立てる。
func sandboxSpec(prepared Prepared, values secret.Values) (runtime.SandboxSpec, error) {
	secrets, err := prepared.Wiring.SandboxSecrets(values)
	if err != nil {
		return runtime.SandboxSpec{}, err
	}
	spec := runtime.SandboxSpec{
		Name:        prepared.Target.Name,
		Repo:        prepared.Target.Repo,
		Env:         prepared.VMEnv,
		Secrets:     secrets,
		EgressRules: prepared.Declaration.SandboxEgress,
		// boot を宣言していなくても再生を頼む (script を置かなければ何も走らない)。再生の仕組みの無い VM を
		// 実 sbx で確かめていないので、作る VM の形を変えない
		ReplayBoot: true,
	}
	if pin := prepared.Declaration.Herdr; pin != nil {
		spec.Herdr = &runtime.HerdrInstall{Version: pin.Version}
	}
	return spec, nil
}

// createdStageError は VM を作れた後の段の失敗を、表示する段の error にする。VM を作れなかった失敗はそのまま返す。
// 知らない段でも *StageError にする (VM が残っていることを呼び出し側へ落とさない)。
func createdStageError(err error) error {
	var created *runtime.CreatedError
	if !errors.As(err, &created) {
		return err
	}
	switch created.Step {
	case runtime.CreatedStepSandboxEgress:
		return stageError(StageSandboxEgress, created.Err)
	case runtime.CreatedStepHerdrStartup:
		return stageError(StageHerdr, created.Err)
	}
	return stageError(Stage(fmt.Sprintf("sandbox VM を作った後の段 %d", created.Step)), created.Err)
}

// RunningPolicy は稼働中の VM を撤去するか。
type RunningPolicy int

const (
	// RefuseRunning は稼働中の VM を撤去しない。sbx は使用中かを区別できず、撤去は使用中の VM も消すため (ADR 0006)。
	RefuseRunning RunningPolicy = iota
	// RemoveRunning は稼働中 (使用中かもしれない) の VM も撤去する (--force)。
	RemoveRunning
)

// RunningError は稼働中の VM を RefuseRunning で撤去しようとしたときの error。
type RunningError struct {
	Status runtime.SandboxStatus
}

func (e *RunningError) Error() string {
	return fmt.Sprintf("sandbox VM が稼働中 (%s)", e.Status)
}

// Destroy は sandbox VM を消し、cache clone と状態ディレクトリを片付ける。
// 撤去の直前に状態を読み直し、稼働中なら running に従う。VM が無くても env rm を呼ぶ (作成前に置いた sandbox スコープの secret を消すため。ADR 0006)。
// VM を消せなければ、env 定義を残すために状態ディレクトリを消さずに止める。その後段の失敗は warnings に集めて撤去を続ける。
func Destroy(ctx context.Context, hosts Hosts, places Places, target Target, running RunningPolicy) (warnings []error, err error) {
	rt := hosts.Runtime
	inspection, err := Inspect(ctx, rt, places, target)
	if err != nil {
		return nil, err
	}
	if err := inspection.RequireManaged(target.Name); err != nil {
		return nil, err
	}
	if !inspection.NotRunning() && running == RefuseRunning {
		return nil, &RunningError{Status: inspection.Status}
	}
	stateDir := places.StateDir(target.Name)
	// herdr machine の解除は VM を消す前に行う。失敗しても撤去は続ける
	if err := removeHerdrMachine(ctx, hosts, places, target.Name); err != nil {
		warnings = append(warnings, err)
	}
	if err := rt.RemoveEnvironment(ctx, stateDir); err != nil {
		return warnings, fmt.Errorf("sandbox VM %s を消せない (状態ディレクトリ %s は残した): %w", target.Name, stateDir, err)
	}
	if target.FromGitURL() {
		if err := DiscardClone(places, target); err != nil {
			warnings = append(warnings, err)
		}
	}
	if err := os.RemoveAll(stateDir); err != nil {
		warnings = append(warnings, fmt.Errorf("状態ディレクトリ %s を消せない: %w", stateDir, err))
	}
	return warnings, nil
}

// DiscardClone は git URL の Target の cache clone を消す。cacheRoot の直下にあるものだけを消す (利用者の repo を消さないため)。
func DiscardClone(places Places, target Target) error {
	if !target.FromGitURL() || filepath.Dir(filepath.Clean(target.Repo)) != filepath.Clean(places.CacheRoot) {
		return fmt.Errorf("cache clone でない %s は消さなかった", target.Repo)
	}
	if err := os.RemoveAll(target.Repo); err != nil {
		return fmt.Errorf("cache clone %s を消せない: %w", target.Repo, err)
	}
	return nil
}

// Summary は確認関門と plan で見せる merge 結果。
func (p Prepared) Summary() (string, error) {
	type skipped struct {
		Name        string   `yaml:"name"`
		DeniedHosts []string `yaml:"denied_hosts"`
	}
	view := struct {
		Sandbox           string            `yaml:"sandbox"`
		Repo              string            `yaml:"repo"`
		URL               string            `yaml:"url,omitempty"`
		Declaration       Declaration       `yaml:"declaration"`
		VMEnv             map[string]string `yaml:"vm_env,omitempty"`
		SkippedSecrets    []skipped         `yaml:"skipped_secrets,omitempty"`
		DroppedRepoEgress []string          `yaml:"dropped_repo_egress,omitempty"`
		GlobalEgress      []string          `yaml:"global_egress"`
	}{
		Sandbox: p.Target.Name, Repo: p.Target.Repo, URL: p.Target.URL,
		Declaration: p.Declaration, VMEnv: p.VMEnv, DroppedRepoEgress: p.DroppedRepoEgress, GlobalEgress: p.GlobalEgress,
	}
	for _, skip := range p.Wiring.Skipped {
		view.SkippedSecrets = append(view.SkippedSecrets, skipped{Name: skip.Name, DeniedHosts: skip.DeniedHosts})
	}
	data, err := yaml.Marshal(view)
	return string(data), err
}
