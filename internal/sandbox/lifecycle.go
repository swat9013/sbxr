package sandbox

import (
	"context"
	"errors"
	"fmt"
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

// 状態ディレクトリに置くファイル。
const (
	envFile         = "sbxenv.yaml"
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
}

// RepoEgressPolicy は repo 宣言の egress を sandbox スコープ rule にするか。
type RepoEgressPolicy int

const (
	// KeepRepoEgress は repo の egress を sandbox スコープ rule にする。
	KeepRepoEgress RepoEgressPolicy = iota
	// DropRepoEgress は repo の egress を捨てる。人間が確認関門で見ていない untrusted な宣言から宛先を開けないため。
	DropRepoEgress
)

// Prepare は target を用意し (git URL なら clone)、3 スコープの宣言を merge して作る内容を確定する。
func Prepare(ctx context.Context, clone Cloner, places Places, target Target, repoEgress RepoEgressPolicy) (Prepared, error) {
	if err := ensureClone(ctx, clone, target); err != nil {
		return Prepared{}, fmt.Errorf("%s を clone できない: %w", target.URL, err)
	}
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
	prepared := Prepared{Target: target, GlobalEgress: egress.DesiredResources(globalGroups)}
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

// Managed は sandbox VM の状態ディレクトリがあるか (sbxr が作った VM か) を返す。
func Managed(places Places, name string) (bool, error) {
	_, err := os.Stat(filepath.Join(places.StateDir(name), envFile))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Create は状態ディレクトリを書き、secret を配線し、sandbox VM を作って sandbox スコープ rule を足す。
// secret は作成前に置く (作成時に VM の環境変数へ placeholder が入る)。rule は作成後にしか置けない (ADR 0006 の実測)。
// 途中で失敗したら状態ディレクトリを残す (destroy がそれを使って片付ける)。
func Create(ctx context.Context, rt runtime.Runtime, places Places, prepared Prepared, values secret.Values) error {
	name := prepared.Target.Name
	stateDir := places.StateDir(name)
	if err := writeState(stateDir, prepared); err != nil {
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
			return fmt.Errorf("sandbox スコープ rule %s を足せない: %w", resource, err)
		}
	}
	return nil
}

// envDefinition は sbx env create に渡す env 定義。repo は VM 内の clone として渡す (host の作業ツリーを書き換えさせない)。
type envDefinition struct {
	SchemaVersion string            `yaml:"schemaVersion"`
	Agent         string            `yaml:"agent"`
	Name          string            `yaml:"name"`
	Workspace     envWorkspace      `yaml:"workspace"`
	Env           map[string]string `yaml:"env,omitempty"`
}

type envWorkspace struct {
	Path  string `yaml:"path"`
	Clone bool   `yaml:"clone"`
}

func writeState(dir string, prepared Prepared) error {
	env, err := yaml.Marshal(envDefinition{
		SchemaVersion: "1",
		Agent:         "claude",
		Name:          prepared.Target.Name,
		Workspace:     envWorkspace{Path: prepared.Target.Repo, Clone: true},
		Env:           prepared.VMEnv,
	})
	if err != nil {
		return err
	}
	decl, err := yaml.Marshal(prepared.Declaration)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("状態ディレクトリ %s を作れない: %w", dir, err)
	}
	for file, data := range map[string][]byte{envFile: env, declarationFile: decl} {
		if err := os.WriteFile(filepath.Join(dir, file), data, 0o600); err != nil {
			return fmt.Errorf("状態ディレクトリに %s を書けない: %w", file, err)
		}
	}
	return nil
}

// Destroy は sandbox VM を消し、cache clone と状態ディレクトリを片付ける。呼び出し側は VM が止まっていることを確かめてから呼ぶ。
// VM を消せなければ、env 定義を残すために状態ディレクトリを消さずに止める。その後段の失敗は warnings に集めて撤去を続ける。
func Destroy(ctx context.Context, rt runtime.Runtime, places Places, target Target) (warnings []error, err error) {
	stateDir := places.StateDir(target.Name)
	status, err := rt.SandboxStatus(ctx, target.Name)
	if err != nil {
		return nil, err
	}
	if status != runtime.SandboxAbsent {
		if err := rt.RemoveEnvironment(ctx, stateDir); err != nil {
			return nil, fmt.Errorf("sandbox VM %s を消せない (状態ディレクトリ %s は残した): %w", target.Name, stateDir, err)
		}
	}
	if target.FromGitURL() {
		if err := removeCacheClone(places.CacheRoot, target.Repo); err != nil {
			warnings = append(warnings, err)
		}
	}
	if err := os.RemoveAll(stateDir); err != nil {
		warnings = append(warnings, fmt.Errorf("状態ディレクトリ %s を消せない: %w", stateDir, err))
	}
	return warnings, nil
}

// removeCacheClone は cacheRoot の直下にある clone だけを消す。利用者の repo を消さないための確かめ。
func removeCacheClone(cacheRoot, repo string) error {
	if filepath.Dir(filepath.Clean(repo)) != filepath.Clean(cacheRoot) {
		return fmt.Errorf("cache clone %s が %s の直下に無いので消さなかった", repo, cacheRoot)
	}
	if err := os.RemoveAll(repo); err != nil {
		return fmt.Errorf("cache clone %s を消せない: %w", repo, err)
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
