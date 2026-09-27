// Package inmemory は test 用の in-memory の runtime.Runtime。domain の test が sbx の引数を見ずに、
// 実行基盤に置かれた状態 (global rule・sandbox VM・sandbox スコープの secret と rule・VM のファイル) で結果を確かめるために使う。
// 振る舞いは Sbx adapter と同じ契約 test (runtime/runtimetest) で揃える。本番の CLI には組み込まない (ADR 0005)。
package inmemory

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/swat9013/sbxr/internal/runtime"
)

// Runtime は実行基盤の状態を memory に持つ。
type Runtime struct {
	// GlobalRules は global rule。
	GlobalRules []runtime.EgressRule
	// Sandboxes は sandbox VM の名前ごとの状態。VM の作成前に置いた secret も、状態 absent の entry として持つ。
	Sandboxes map[string]*Sandbox
	// Commands は ExecInSandbox で走ったコマンドを起きた順に並べる。
	Commands []Command
	// Respond は VM 内のコマンドへの応答。nil なら空の出力で成功する。
	Respond func(sandbox string, command runtime.SandboxCommand) ([]byte, error)
	// FailOnGlobalWrite が n (1 始まり) なら、global rule への n 回目の書き込みを失敗させる。0 なら失敗させない。
	FailOnGlobalWrite int
	// DropGlobalWrites が true なら、global rule への書き込みを受けたふりをして反映しない (適用が効かない実行基盤の再現)。
	DropGlobalWrites bool

	globalWrites int
	nextID       int
}

// Sandbox は 1 つの sandbox VM に置かれたもの。
type Sandbox struct {
	Status  runtime.SandboxStatus
	Secrets []runtime.SandboxSecret
	// EgressRules は sandbox スコープ rule の宛先。
	EgressRules []string
	// Files は VM 内のファイル (絶対 path → 中身と mode)。
	Files map[string]File
}

// File は VM 内の 1 つのファイル。
type File struct {
	Data []byte
	// Mode は書き込みで指定した mode。指定が無ければ 0。
	Mode fs.FileMode
}

// Command は VM 内で走ったコマンド。
type Command struct {
	Sandbox string
	runtime.SandboxCommand
}

var _ runtime.Runtime = (*Runtime)(nil)

// New は空の実行基盤を返す。
func New() *Runtime {
	return &Runtime{Sandboxes: map[string]*Sandbox{}}
}

// Sandbox は name の sandbox VM を返す。無ければ状態 absent の空の entry を作って返す。
func (r *Runtime) Sandbox(name string) *Sandbox {
	if r.Sandboxes == nil {
		r.Sandboxes = map[string]*Sandbox{}
	}
	sb, ok := r.Sandboxes[name]
	if !ok {
		sb = &Sandbox{Status: runtime.SandboxAbsent, Files: map[string]File{}}
		r.Sandboxes[name] = sb
	}
	return sb
}

// Start は sbxr の外で止まった VM が起動したこと (herdr の繋ぎ直しや sbx exec) を再現する。
func (r *Runtime) Start(name string) error {
	sb := r.Sandbox(name)
	if sb.Status == runtime.SandboxAbsent {
		return fmt.Errorf("inmemory: sandbox %s が無い", name)
	}
	sb.Status = runtime.SandboxRunning
	return nil
}

func (r *Runtime) ListGlobalEgressRules(context.Context) ([]runtime.EgressRule, error) {
	return slices.Clone(r.GlobalRules), nil
}

func (r *Runtime) AllowGlobalEgress(_ context.Context, resource string) error {
	if err := r.globalWrite(); err != nil || r.DropGlobalWrites {
		return err
	}
	r.nextID++
	r.GlobalRules = append(r.GlobalRules, runtime.EgressRule{ID: fmt.Sprintf("added-%d", r.nextID), Decision: runtime.DecisionAllow, Resources: []string{resource}})
	return nil
}

func (r *Runtime) RemoveGlobalEgressRule(_ context.Context, id string) error {
	if err := r.globalWrite(); err != nil || r.DropGlobalWrites {
		return err
	}
	i := slices.IndexFunc(r.GlobalRules, func(rule runtime.EgressRule) bool { return rule.ID == id })
	if i < 0 {
		return fmt.Errorf("inmemory: rule %s が無い", id)
	}
	r.GlobalRules = slices.Delete(r.GlobalRules, i, i+1)
	return nil
}

func (r *Runtime) globalWrite() error {
	r.globalWrites++
	if r.globalWrites == r.FailOnGlobalWrite {
		return fmt.Errorf("inmemory: %d 回目の global rule の書き込みを失敗させた", r.FailOnGlobalWrite)
	}
	return nil
}

func (r *Runtime) SetSandboxSecret(_ context.Context, sandbox string, secret runtime.SandboxSecret) error {
	sb := r.Sandbox(sandbox)
	sb.Secrets = append(sb.Secrets, secret)
	return nil
}

func (r *Runtime) SandboxStatus(_ context.Context, sandbox string) (runtime.SandboxStatus, error) {
	if sb, ok := r.Sandboxes[sandbox]; ok {
		return sb.Status, nil
	}
	return runtime.SandboxAbsent, nil
}

func (r *Runtime) CreateEnvironment(_ context.Context, envDir string) error {
	name, err := envName(envDir)
	if err != nil {
		return err
	}
	r.Sandbox(name).Status = runtime.SandboxRunning
	return nil
}

// RemoveEnvironment は VM を、置かれた secret と rule ごと消す。VM が無くても secret を消して成功する (sbx の実測)。
func (r *Runtime) RemoveEnvironment(_ context.Context, envDir string) error {
	name, err := envName(envDir)
	if err != nil {
		return err
	}
	delete(r.Sandboxes, name)
	return nil
}

func (r *Runtime) StopSandbox(_ context.Context, sandbox string) error {
	sb, ok := r.Sandboxes[sandbox]
	if !ok || sb.Status == runtime.SandboxAbsent {
		return fmt.Errorf("inmemory: sandbox %s が無い", sandbox)
	}
	sb.Status = runtime.SandboxStopped
	return nil
}

// AllowSandboxEgress は sandbox スコープ rule を足す。VM の作成前には置けない (sbx の実測)。
func (r *Runtime) AllowSandboxEgress(_ context.Context, sandbox, resource string) error {
	sb, ok := r.Sandboxes[sandbox]
	if !ok || sb.Status == runtime.SandboxAbsent {
		return fmt.Errorf("inmemory: sandbox %s が無い", sandbox)
	}
	sb.EgressRules = append(sb.EgressRules, resource)
	return nil
}

func (r *Runtime) ExecInSandbox(_ context.Context, sandbox string, command runtime.SandboxCommand) ([]byte, error) {
	if _, err := r.running(sandbox); err != nil {
		return nil, err
	}
	r.Commands = append(r.Commands, Command{Sandbox: sandbox, SandboxCommand: command})
	if r.Respond == nil {
		return nil, nil
	}
	return r.Respond(sandbox, command)
}

func (r *Runtime) ReadSandboxFile(_ context.Context, sandbox, path string) ([]byte, error) {
	sb, err := r.running(sandbox)
	if err != nil {
		return nil, err
	}
	file, ok := sb.Files[path]
	if !ok {
		return nil, fmt.Errorf("inmemory: %s が無い", path)
	}
	return slices.Clone(file.Data), nil
}

func (r *Runtime) WriteSandboxFile(_ context.Context, sandbox, path string, data []byte, mode fs.FileMode) error {
	sb, err := r.running(sandbox)
	if err != nil {
		return err
	}
	sb.Files[path] = File{Data: slices.Clone(data), Mode: mode}
	return nil
}

func (r *Runtime) SandboxFileExists(_ context.Context, sandbox, path string) (bool, error) {
	sb, err := r.running(sandbox)
	if err != nil {
		return false, err
	}
	_, ok := sb.Files[path]
	return ok, nil
}

func (r *Runtime) SSHTarget(sandbox string) string {
	return sandbox + ".inmemory"
}

// running は稼働中の VM を返す。VM の中の操作は稼働中の VM にしか届かない。
func (r *Runtime) running(sandbox string) (*Sandbox, error) {
	sb, ok := r.Sandboxes[sandbox]
	if !ok || sb.Status != runtime.SandboxRunning {
		return nil, fmt.Errorf("inmemory: sandbox %s が動いていない", sandbox)
	}
	return sb, nil
}

// envName は env 定義 (<dir>/sbxenv.yaml) の name を読む。env 定義が無ければ error (sbx の実測)。
func envName(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "sbxenv.yaml"))
	if err != nil {
		return "", fmt.Errorf("inmemory: no sbxenv.yaml found at %s: %w", dir, err)
	}
	var env struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil || env.Name == "" {
		return "", fmt.Errorf("inmemory: %s/sbxenv.yaml の name を読めない", dir)
	}
	return env.Name, nil
}
