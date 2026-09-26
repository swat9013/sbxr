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

	"github.com/swat9013/sbxr/internal/assets"
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

// 状態ディレクトリに置くファイル。declaration.yaml は作成がすべて済んでから書き、作成が終わった印にする。
const (
	envFile         = "sbxenv.yaml"
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
	// Secrets は配線する secret。
	Secrets []WiredSecret `yaml:"secrets"`
}

// WiredSecret は配線する secret のうち、VM に効く部分。値は持たない。
type WiredSecret struct {
	Name    string   `yaml:"name"`
	Service string   `yaml:"service,omitempty"`
	Hosts   []string `yaml:"hosts"`
	Env     string   `yaml:"env,omitempty"`
}

// Prepared は create の確認関門で見せ、承認後に作る内容。
type Prepared struct {
	Target      Target
	Declaration Declaration
	// GlobalEgress は全 sandbox VM に効く global rule の宛先 (sbxr policy sync が収束させる)。
	GlobalEgress []string
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
	prepared := Prepared{Target: target, GlobalEgress: egress.DesiredResources(globalGroups), OriginHost: host}
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
	for _, wire := range prepared.Wiring.Wired {
		decl.Secrets = append(decl.Secrets, WiredSecret{Name: wire.Name, Service: wire.Definition.Service, Hosts: wire.Definition.Hosts, Env: wire.Definition.Env})
	}
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
	recorded, err := os.ReadFile(filepath.Join(dir, sourceFile))
	switch {
	case errors.Is(err, fs.ErrNotExist) && status == runtime.SandboxAbsent:
		return Inspection{Situation: Absent, Status: status}, nil
	case errors.Is(err, fs.ErrNotExist):
		return Inspection{Situation: Unmanaged, Status: status}, nil
	case err != nil:
		return Inspection{}, fmt.Errorf("状態ディレクトリ %s を読めない: %w", dir, err)
	case string(recorded) != target.Source():
		return Inspection{Situation: OtherSource, Status: status, recordedSource: string(recorded)}, nil
	}
	if _, err := os.Stat(filepath.Join(dir, declarationFile)); errors.Is(err, fs.ErrNotExist) {
		return Inspection{Situation: Incomplete, Status: status}, nil
	} else if err != nil {
		return Inspection{}, err
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

// Create は状態ディレクトリを書き、secret を配線し、sandbox VM を作って sandbox スコープ rule を足し、VM の中を宣言どおりにする
// (materialize → read-back → init → boot)。secret は作成前に置く (作成時に VM の環境変数へ placeholder が入る)。
// rule は作成後にしか置けない (ADR 0006 の実測)。途中で失敗したら状態ディレクトリと VM を残す (destroy がそれを使って片付ける)。
// 作成が終わった印 (declaration.yaml) は最後に書く。VM の中の段の失敗は *StageError で返す。
func Create(ctx context.Context, rt runtime.Runtime, places Places, prepared Prepared, values secret.Values, progress io.Writer) error {
	name := prepared.Target.Name
	stateDir := places.StateDir(name)
	if err := writeEnvironment(stateDir, prepared); err != nil {
		return err
	}
	if err := secret.Apply(ctx, rt, name, prepared.Wiring, values); err != nil {
		return err
	}
	if err := rt.CreateEnvironment(ctx, stateDir); err != nil {
		return fmt.Errorf("sandbox VM %s を作れない: %w", name, err)
	}
	for _, resource := range prepared.Declaration.SandboxEgress {
		if err := rt.AllowSandboxEgress(ctx, name, resource); err != nil {
			return stageError(StageSandboxEgress, fmt.Errorf("%s を足せない: %w", resource, err))
		}
	}
	if err := setUpInside(ctx, rt, prepared, progress); err != nil {
		return err
	}
	return writeDeclaration(stateDir, prepared.Declaration)
}

// envDefinition は sbx env create に渡す env 定義。repo は VM 内の clone として渡す (host の作業ツリーを書き換えさせない)。
// agent は Claude Code に固定する (agent runtime profile は Claude Code の settings.json だけを扱う)。
type envDefinition struct {
	SchemaVersion string            `yaml:"schemaVersion"`
	Agent         string            `yaml:"agent"`
	Name          string            `yaml:"name"`
	Workspace     envWorkspace      `yaml:"workspace"`
	Kits          []string          `yaml:"kits"`
	Env           map[string]string `yaml:"env,omitempty"`
}

// kitsDir は状態ディレクトリの中で埋め込みの kit を置くディレクトリ。env 定義からは相対 path で指す
// (sbx は ./ で始まる kit を env 定義のディレクトリ基準で解決する)。
const kitsDir = "kits"

type envWorkspace struct {
	Path  string `yaml:"path"`
	Clone bool   `yaml:"clone"`
}

func writeEnvironment(dir string, prepared Prepared) error {
	env, err := yaml.Marshal(envDefinition{
		SchemaVersion: "1",
		Agent:         "claude",
		Name:          prepared.Target.Name,
		Workspace:     envWorkspace{Path: prepared.Target.Repo, Clone: true},
		Kits:          kitReferences(),
		Env:           prepared.VMEnv,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("状態ディレクトリ %s を作れない: %w", dir, err)
	}
	// CopyFS は既存のファイルを上書きしないので、前回の残りを消してから書く
	if err := os.RemoveAll(filepath.Join(dir, kitsDir)); err != nil {
		return fmt.Errorf("状態ディレクトリの kit を書き直せない: %w", err)
	}
	if err := os.CopyFS(filepath.Join(dir, kitsDir), assets.Kits()); err != nil {
		return fmt.Errorf("状態ディレクトリに kit を書けない: %w", err)
	}
	for _, file := range []struct {
		name string
		data []byte
	}{{envFile, env}, {sourceFile, []byte(prepared.Target.Source())}} {
		if err := os.WriteFile(filepath.Join(dir, file.name), file.data, 0o600); err != nil {
			return fmt.Errorf("状態ディレクトリに %s を書けない: %w", file.name, err)
		}
	}
	return nil
}

// kitReferences は埋め込みの kit を env 定義から指す相対 path。
func kitReferences() []string {
	entries, err := fs.ReadDir(assets.Kits(), ".")
	if err != nil {
		panic(err) // 埋め込みの資材は build 時に決まる
	}
	var refs []string
	for _, entry := range entries {
		refs = append(refs, "./"+kitsDir+"/"+entry.Name())
	}
	return refs
}

func writeDeclaration(dir string, decl Declaration) error {
	data, err := yaml.Marshal(decl)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, declarationFile), data, 0o600); err != nil {
		return fmt.Errorf("状態ディレクトリに %s を書けない: %w", declarationFile, err)
	}
	return nil
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
func Destroy(ctx context.Context, rt runtime.Runtime, places Places, target Target, running RunningPolicy) (warnings []error, err error) {
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
	if err := rt.RemoveEnvironment(ctx, stateDir); err != nil {
		return nil, fmt.Errorf("sandbox VM %s を消せない (状態ディレクトリ %s は残した): %w", target.Name, stateDir, err)
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
