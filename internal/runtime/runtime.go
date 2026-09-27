// Package runtime は sandbox VM の実行基盤との境界を置く (ADR 0005)。本番の実行基盤は sbx だけで、
// test は in-memory の adapter (runtime/inmemory) を使う。2 つの adapter は runtime/runtimetest の契約 test で揃える。
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
	// SetSandboxSecret は 1 つの sandbox VM に限った secret を置く。sandbox VM の destroy で消える。
	SetSandboxSecret(ctx context.Context, sandbox string, secret SandboxSecret) error

	// SandboxStatus は sandbox VM の状態を返す。無ければ SandboxAbsent。
	SandboxStatus(ctx context.Context, sandbox string) (SandboxStatus, error)
	// CreateEnvironment は envDir の env 定義から sandbox VM を作り、起動する。
	CreateEnvironment(ctx context.Context, envDir string) error
	// RemoveEnvironment は envDir の env 定義が指す sandbox VM を、sandbox スコープの secret と rule ごと消す。
	// VM が無くても sandbox スコープの secret は消す。使用中の VM も消す。
	RemoveEnvironment(ctx context.Context, envDir string) error
	// StopSandbox は sandbox VM を止める。状態は残る。
	StopSandbox(ctx context.Context, sandbox string) error
	// AllowSandboxEgress は 1 つの宛先を許可する sandbox スコープ rule を足す。sandbox VM の作成後にしか置けない。
	AllowSandboxEgress(ctx context.Context, sandbox, resource string) error
	// ExecInSandbox は sandbox VM 内でコマンドを実行し、stdout を返す。0 以外で終われば error。
	ExecInSandbox(ctx context.Context, sandbox string, command SandboxCommand) ([]byte, error)
	// ReadSandboxFile は sandbox VM 内の path (絶対 path) の中身を返す。無ければ error。
	ReadSandboxFile(ctx context.Context, sandbox, path string) ([]byte, error)
	// WriteSandboxFile は sandbox VM 内の path (絶対 path) に data を書く。親ディレクトリが無ければ作る。
	// VM の agent が読み書きできる持ち主で置く。mode が 0 なら mode を指定しない。
	WriteSandboxFile(ctx context.Context, sandbox, path string, data []byte, mode fs.FileMode) error
	// SandboxFileExists は sandbox VM 内に path (絶対 path) があるかを返す。確かめられなければ error (「無い」とは区別する)。
	SandboxFileExists(ctx context.Context, sandbox, path string) (bool, error)
	// SSHTarget は host から sandbox VM へ ssh で繋ぐ宛先 (herdr machine の登録先)。
	SSHTarget(sandbox string) string
}

// SandboxCommand は sandbox VM 内で実行するコマンド。
type SandboxCommand struct {
	// Args は実行するコマンドと引数。shell を通さない (shell が要るときは Args に sh -c を書く)。
	Args []string
	// Dir は VM 内の作業ディレクトリ。空なら実行基盤の既定。
	Dir string
	// Input はコマンドの stdin に渡す内容。nil なら何も渡さない。
	Input []byte
}

// SandboxStatus は sandbox VM の状態。実行基盤の値をどの状態と読むかは adapter が決める。
type SandboxStatus int

const (
	// SandboxAbsent は VM が無い。
	SandboxAbsent SandboxStatus = iota
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
