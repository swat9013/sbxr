// Package runtime は sandbox VM の実行基盤との境界を置く (ADR 0005)。本番の実行基盤は sbx だけ。
// domain の test は in-memory の adapter (runtime/inmemory) を、Sbx adapter と CLI の test は sbx stub を使う。
// 2 つの adapter は runtime/runtimetest の契約 test で揃える。
package runtime

import (
	"context"
	"fmt"
	"io/fs"
)

// Runtime は sbxr が実行基盤に求める操作。
type Runtime interface {
	// ListGlobalEgressRules は全 sandbox VM に効く global rule のうち、sbxr が収束させてよいものを返す。
	// 実行基盤自身が管理する rule と sandbox スコープ rule は含めない。
	ListGlobalEgressRules(ctx context.Context) ([]EgressRule, error)
	// AllowGlobalEgress は 1 つの宛先を許可する global rule を足す。
	AllowGlobalEgress(ctx context.Context, resource string) error
	// RemoveGlobalEgressRule は global rule を 1 つ消す。
	RemoveGlobalEgressRule(ctx context.Context, id string) error

	// SandboxStatus は sandbox VM の状態を返す。無ければ SandboxAbsent。
	SandboxStatus(ctx context.Context, sandbox string) (SandboxStatus, error)
	// DefineSandbox は stateDir に、spec の sandbox VM を作るための定義を書く (sbx では env 定義と kit)。
	// 前の定義があれば置き換える。secret の値は定義に書かない。作成 (CreateSandbox) と撤去 (RemoveEnvironment) はこの定義を使う。
	DefineSandbox(stateDir string, spec SandboxSpec) error
	// CreateSandbox は stateDir の定義から spec の sandbox VM を作り、起動する。sandbox スコープの secret と rule を置き、
	// herdr を導入するなら VM の起動時の処理が終わるまで待つ。secret と rule をどの順で置くかは adapter が決める。
	// VM を作れた後の段で失敗したら *CreatedError を返す (VM は残っている)。
	CreateSandbox(ctx context.Context, stateDir string, spec SandboxSpec) error
	// DefinedWithHerdr は stateDir の定義が herdr を導入するかを返す。定義が無ければ false。
	// 定義は作成の最初に書くので、作成途中の VM でも読める。
	DefinedWithHerdr(stateDir string) (bool, error)
	// RemoveEnvironment は envDir の定義が指す sandbox VM を、sandbox スコープの secret と rule ごと消す。
	// VM が無くても sandbox スコープの secret は消す。使用中の VM も消す。定義が無ければ error。
	RemoveEnvironment(ctx context.Context, envDir string) error
	// StopSandbox は sandbox VM を止める。状態は残る。
	StopSandbox(ctx context.Context, sandbox string) error
	// ExecInSandbox は sandbox VM 内でコマンドを実行し、stdout を返す。0 以外で終われば error。
	ExecInSandbox(ctx context.Context, sandbox string, command SandboxCommand) ([]byte, error)
	// ReadSandboxFile は sandbox VM 内の path (絶対 path) の中身を返す。無ければ error。
	ReadSandboxFile(ctx context.Context, sandbox, path string) ([]byte, error)
	// WriteSandboxFile は sandbox VM 内の path (絶対 path) に data を書く。親ディレクトリが無ければ作る。
	// VM の agent が読み書きできる持ち主で置く。mode が KeepMode なら mode を変えない。
	WriteSandboxFile(ctx context.Context, sandbox, path string, data []byte, mode fs.FileMode) error
	// SandboxFileExists は sandbox VM 内に path (絶対 path) があるかを返す。確かめられなければ error (「無い」とは区別する)。
	SandboxFileExists(ctx context.Context, sandbox, path string) (bool, error)
	// SSHTarget は host から sandbox VM へ ssh で繋ぐ宛先 (herdr machine の登録先)。
	SSHTarget(sandbox string) string
}

// SandboxSpec は sandbox VM の作る内容 (decision/0009)。
type SandboxSpec struct {
	Name string
	// Repo は VM 内に clone する host の repo。
	Repo string
	// Env は VM の環境変数 (配線した secret の付随値)。
	Env map[string]string
	// Herdr は VM に導入する herdr。nil なら導入しない。
	Herdr *HerdrInstall
	// Secrets は sandbox VM に限って置く secret (値を含む)。
	Secrets []SandboxSecret
	// EgressRules は sandbox スコープ rule の宛先。
	EgressRules []string
}

// HerdrInstall は VM に導入する herdr の版。
type HerdrInstall struct {
	Version string
}

// BootScriptRelPath は VM の agent user の home からの、起動ごとに再生する boot script の置き場。
// adapter は VM の起動ごとにここにある script を実行する (無ければ何もしない)。
const BootScriptRelPath = ".config/sbxr/boot.sh"

// CreatedStep は VM を作れた後の段。
type CreatedStep int

const (
	// CreatedStepSandboxEgress は sandbox スコープ rule を足す段。
	CreatedStepSandboxEgress CreatedStep = iota + 1
	// CreatedStepStartup は VM の起動時の処理 (herdr の導入) が終わるのを待つ段。
	CreatedStepStartup
)

// CreatedError は CreateSandbox が VM を作れた後の段で失敗したときの error。VM は残っている。
type CreatedError struct {
	Step CreatedStep
	Err  error
}

func (e *CreatedError) Error() string { return e.Err.Error() }
func (e *CreatedError) Unwrap() error { return e.Err }

// SandboxCommand は sandbox VM 内で実行するコマンド。
type SandboxCommand struct {
	// Args は実行するコマンドと引数。shell を通さない (shell が要るときは Args に sh -c を書く)。
	Args []string
	// Dir は VM 内の作業ディレクトリ。空なら実行基盤の既定。
	Dir string
	// Input はコマンドの stdin に渡す内容。nil なら何も渡さない。
	Input []byte
}

// KeepMode は WriteSandboxFile で mode を変えない (既存のファイルの mode を保ち、新しいファイルは既定の mode になる)。
// 値は 0 なので、mode 0000 で置くことは表せない (sbxr が VM に置くファイルは agent が読むので、0000 は要らない)。
const KeepMode fs.FileMode = 0

// SandboxStatus は sandbox VM の状態。実行基盤の値をどの状態と読むかは adapter が決める。
// ゼロ値はどの状態でもない (状態を得られなかったときの値を、VM が無いとも止まっているとも読ませない)。
type SandboxStatus int

const (
	// SandboxUnknown はどの状態でもない (状態を得られなかった)。ゼロ値。
	SandboxUnknown SandboxStatus = iota
	// SandboxAbsent は VM が無い。
	SandboxAbsent
	// SandboxStopped は VM が止まっている。
	SandboxStopped
	// SandboxRunning は VM が止まっていない (使用中かもしれない)。
	SandboxRunning
)

func (s SandboxStatus) String() string {
	switch s {
	case SandboxAbsent:
		return "absent"
	case SandboxStopped:
		return "stopped"
	case SandboxRunning:
		return "running"
	case SandboxUnknown:
		return "unknown"
	}
	return fmt.Sprintf("SandboxStatus(%d)", int(s))
}

// SandboxSecret は sandbox VM に配線する secret。VM には placeholder だけが入り、実値は host 側の proxy が差し込む。
type SandboxSecret struct {
	// Service は実行基盤の組み込み service の名前 (例: github)。空なら Hosts と Env による placeholder 注入。
	Service string
	// Hosts は placeholder 注入で実値を差し込む宛先。
	Hosts []string
	// Env は placeholder を入れる VM の環境変数名。
	Env   string
	Value string
}

// EgressRule は実行基盤にある egress の rule。
type EgressRule struct {
	ID        string
	Decision  Decision
	Resources []string
}

// Decision は rule が宛先を許可するか拒否するか。
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)
