// Package inmemory は test 用の in-memory の runtime.Runtime。domain の test が sbx の引数を見ずに、
// 実行基盤に置かれた状態 (global rule・sandbox VM・sandbox スコープの secret と rule・VM のファイル) で結果を確かめるために使う。
// 振る舞いは Sbx adapter と同じ契約 test (runtime/runtimetest) で揃える。本番の CLI には組み込まない (ADR 0005)。
package inmemory

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/swat9013/sbxr/internal/runtime"
)

// Runtime は実行基盤の状態を memory に持つ。ただし定義の寿命は、Sbx adapter と同じく状態ディレクトリの実在に従う。
type Runtime struct {
	// GlobalRules は global rule。
	GlobalRules []runtime.EgressRule
	// Sandboxes は sandbox VM の名前ごとの状態。
	Sandboxes map[string]*Sandbox
	// Commands は ExecInSandbox で走ったコマンドを起きた順に並べる。
	Commands []Command
	// Respond は VM 内のコマンドへの応答。nil なら空の出力で成功する。
	Respond func(sandbox string, command runtime.SandboxCommand) ([]byte, error)
	// Definitions は状態ディレクトリごとの定義 (secret の値を除いた作る内容)。状態ディレクトリが消えた定義は無いものとして扱う。
	Definitions map[string]runtime.SandboxSpec
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
	// Mode は書き込みで指定した mode。一度も指定していなければ KeepMode。
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

func (r *Runtime) SandboxStatus(_ context.Context, sandbox string) (runtime.SandboxStatus, error) {
	if sb, ok := r.Sandboxes[sandbox]; ok {
		return sb.Status, nil
	}
	return runtime.SandboxAbsent, nil
}

// DefineSandbox は作る内容を状態ディレクトリごとに覚える (ファイルは書かない)。
// 状態ディレクトリが無ければ error (sbx adapter と同じく、adapter は状態ディレクトリを作らない)。
func (r *Runtime) DefineSandbox(stateDir string, spec runtime.SandboxSpec) error {
	if _, err := os.Stat(stateDir); err != nil {
		return fmt.Errorf("inmemory: 状態ディレクトリ %s が無い: %w", stateDir, err)
	}
	if r.Definitions == nil {
		r.Definitions = map[string]runtime.SandboxSpec{}
	}
	spec.Secrets = nil // 定義に secret の値は残さない
	r.Definitions[stateDir] = spec
	return nil
}

// definition は状態ディレクトリの定義を返す。sbx adapter の定義は状態ディレクトリのファイルなので、
// 状態ディレクトリが消えていれば定義も無い。
func (r *Runtime) definition(stateDir string) (runtime.SandboxSpec, bool) {
	spec, ok := r.Definitions[stateDir]
	if !ok {
		return runtime.SandboxSpec{}, false
	}
	if _, err := os.Stat(stateDir); err != nil {
		return runtime.SandboxSpec{}, false
	}
	return spec, true
}

// CreateSandbox は定義された sandbox VM を作り、secret と rule を置いて稼働中にする。
// 定義が無いか名前が食い違えば、何も置かずに error (sbx adapter と同じ)。
func (r *Runtime) CreateSandbox(_ context.Context, stateDir string, spec runtime.SandboxSpec) error {
	defined, ok := r.definition(stateDir)
	if !ok {
		return fmt.Errorf("inmemory: %s に定義が無い", stateDir)
	}
	if defined.Name != spec.Name {
		return fmt.Errorf("inmemory: 作る内容の名前 %s が定義 (%s) と食い違う", spec.Name, defined.Name)
	}
	sb := r.Sandbox(spec.Name)
	sb.Status = runtime.SandboxRunning
	sb.Secrets = append(sb.Secrets, spec.Secrets...)
	sb.EgressRules = append(sb.EgressRules, spec.EgressRules...)
	return nil
}

// DefinedWithHerdr は覚えた定義が herdr を導入するかを返す。
func (r *Runtime) DefinedWithHerdr(stateDir string) (bool, error) {
	spec, _ := r.definition(stateDir)
	return spec.Herdr != nil, nil
}

// RemoveEnvironment は定義が指す VM を、置かれた secret と rule ごと消す。VM が無くても secret を消して成功する。
// 定義は残す (sbx の env rm は env 定義を消さない)。定義が無ければ error (sbx の実測)。
func (r *Runtime) RemoveEnvironment(_ context.Context, envDir string) error {
	spec, ok := r.definition(envDir)
	if !ok {
		return fmt.Errorf("inmemory: %s に定義が無い", envDir)
	}
	delete(r.Sandboxes, spec.Name)
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
	if sb.Files == nil { // test が Sandbox を直接組み立てたとき
		sb.Files = map[string]File{}
	}
	if mode == runtime.KeepMode {
		mode = sb.Files[path].Mode
	}
	sb.Files[path] = File{Data: slices.Clone(data), Mode: mode}
	return nil
}

func (r *Runtime) SandboxFileExists(_ context.Context, sandbox, path string) (bool, error) {
	sb, err := r.running(sandbox)
	if err != nil {
		return false, err
	}
	// 書いたファイルの親ディレクトリもある (書き込みは親を作る)
	for written := range sb.Files {
		if written == path || strings.HasPrefix(written, strings.TrimSuffix(path, "/")+"/") {
			return true, nil
		}
	}
	return false, nil
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
