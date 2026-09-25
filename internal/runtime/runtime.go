// Package runtime は sandbox VM の実行基盤との境界を置く (ADR 0005)。実装は sbx だけ。
package runtime

import "context"

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
	// 使用中かを確かめずに消すので、呼び出し側が止まっていることを確かめてから呼ぶ。
	RemoveEnvironment(ctx context.Context, envDir string) error
	// StopSandbox は sandbox VM を止める。状態は残る。
	StopSandbox(ctx context.Context, sandbox string) error
	// AllowSandboxEgress は 1 つの宛先を許可する sandbox スコープ rule を足す。sandbox VM の作成後にしか置けない。
	AllowSandboxEgress(ctx context.Context, sandbox, resource string) error
}

// SandboxStatus は sandbox VM の状態。実行基盤が返す値をそのまま持ち、sbxr が扱う値だけを定数にする。
type SandboxStatus string

const (
	SandboxAbsent  SandboxStatus = "absent"
	SandboxRunning SandboxStatus = "running"
	SandboxStopped SandboxStatus = "stopped"
)

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
