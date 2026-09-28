package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/secret"
)

// repoDeclarationFile は repo 宣言のファイル名。
const repoDeclarationFile = "sbxr.yaml"

// sandboxDeclaration は作成時に確定した宣言。状態ディレクトリに残し、drift 検出の基準にする。
type sandboxDeclaration struct {
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
	Herdr *herdrPin `yaml:"herdr,omitempty"`
}

// herdrEnabled は herdr 連携を有効にした宣言か。
func (d sandboxDeclaration) herdrEnabled() bool {
	return d.Herdr != nil
}

// herdrPin は作成時に確定した herdr 連携 (VM に入れる版)。
type herdrPin struct {
	Version string `yaml:"version"`
}

// preparation は create の確認関門で見せ、承認後に作る内容。
type preparation struct {
	Target      sandboxTarget
	Declaration sandboxDeclaration
	// GlobalEgress は全 sandbox VM に効く global rule の宛先 (sbxr policy sync が収束させる)。
	GlobalEgress []string
	// RepoEgress は repo の egress をどう扱って確定したか。作成時の記録に残し、drift を比べるときに同じ扱いを再現する。
	RepoEgress repoEgressPolicy
	// DroppedRepoEgress は git URL を --yes で通したために落とした repo の egress の宛先。
	DroppedRepoEgress []string
	Wiring            secret.Plan
	VMEnv             map[string]string
	// OriginHost は repo の origin の host。VM の git で ssh 形をこの host の https へ書き換える。origin が無ければ空。
	OriginHost string
}

// repoEgressPolicy は repo 宣言の egress を sandbox スコープ rule にするか。
type repoEgressPolicy int

const (
	// keepRepoEgress は repo の egress を sandbox スコープ rule にする。
	keepRepoEgress repoEgressPolicy = iota
	// dropRepoEgress は repo の egress を捨てる。人間が確認関門で見ていない untrusted な宣言から宛先を開けないため。
	dropRepoEgress
)

// loadedDeclaration は 3 スコープの宣言を merge して読んだもの。repo の egress の扱いを決めれば、作る内容が確定する。
// 1 回の読み込みから、扱いの違う作る内容を何度でも出せる (plan は要約と drift の比較で扱いが違う)。
type loadedDeclaration struct {
	target     sandboxTarget
	config     config.Config
	identity   config.Identity
	originHost string
	// warnings は作成を止めないが、利用者に見せる警告。
	warnings []error
}

// loadDeclaration は repoDir の repo 宣言を user 設定と default に重ねて読む。repoDir は target.Repo か、
// git URL を plan のために読む一時 clone (要約に出す path は target.Repo のまま)。
func loadDeclaration(ctx context.Context, userConfig string, target sandboxTarget, repoDir string) (loadedDeclaration, error) {
	if info, err := os.Stat(repoDir); err != nil || !info.IsDir() {
		return loadedDeclaration{}, fmt.Errorf("repo のディレクトリ %s が無い", repoDir)
	}
	host, warning := originHost(ctx, repoDir)
	cfg, err := config.Load(userConfig, filepath.Join(repoDir, repoDeclarationFile))
	if err != nil {
		return loadedDeclaration{}, err
	}
	identity, err := cfg.GitIdentity()
	if err != nil {
		return loadedDeclaration{}, err
	}
	loaded := loadedDeclaration{target: target, config: cfg, identity: identity, originHost: host}
	if warning != nil {
		loaded.warnings = append(loaded.warnings, warning)
	}
	return loaded, nil
}

// prepare は repo の egress を repoEgress で扱って、作る内容を確定する。
func (l loadedDeclaration) prepare(repoEgress repoEgressPolicy) (preparation, error) {
	cfg := l.config
	prepared := preparation{Target: l.target, RepoEgress: repoEgress, GlobalEgress: cfg.GlobalEgress, OriginHost: l.originHost}
	// 落とす repo の egress も config が検証済み (落とすかどうかで plan と create の error を変えない)
	sandboxEgress := cfg.SandboxEgress
	if repoEgress == dropRepoEgress {
		prepared.DroppedRepoEgress, sandboxEgress = sandboxEgress, nil
	}
	var err error
	prepared.Wiring, err = secret.PlanWiring(cfg.Secrets, cfg.SecretDefs, append(slices.Clone(prepared.GlobalEgress), sandboxEgress...))
	if err != nil {
		return preparation{}, err
	}
	prepared.VMEnv, err = prepared.Wiring.VMEnv()
	if err != nil {
		return preparation{}, err
	}

	decl := &prepared.Declaration
	decl.Profile = cfg.Profile
	decl.Git.Name, decl.Git.Email = l.identity.Name, l.identity.Email
	decl.Init, decl.Boot = cfg.Init, cfg.Boot
	decl.SandboxEgress = sandboxEgress
	if cfg.Herdr.Enabled {
		decl.Herdr = &herdrPin{Version: cfg.Herdr.Version}
	}
	decl.Secrets = prepared.Wiring.WiredSecrets()
	return prepared, nil
}

// drift は現在の宣言を作成時と同じ repo の egress の扱いで確定し、作成時の宣言と比べる。
func (d loadedDeclaration) drift(record creationRecord) (Comparison, error) {
	current, err := d.prepare(record.RepoEgress)
	if err != nil {
		return Comparison{}, err
	}
	return record.Drift(current.Declaration)
}

// summary は確認関門と plan で見せる merge 結果。
func (p preparation) summary() (string, error) {
	type skipped struct {
		Name        string   `yaml:"name"`
		DeniedHosts []string `yaml:"denied_hosts"`
	}
	view := struct {
		Sandbox           string             `yaml:"sandbox"`
		Repo              string             `yaml:"repo"`
		URL               string             `yaml:"url,omitempty"`
		Declaration       sandboxDeclaration `yaml:"declaration"`
		VMEnv             map[string]string  `yaml:"vm_env,omitempty"`
		SkippedSecrets    []skipped          `yaml:"skipped_secrets,omitempty"`
		DroppedRepoEgress []string           `yaml:"dropped_repo_egress,omitempty"`
		GlobalEgress      []string           `yaml:"global_egress"`
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
