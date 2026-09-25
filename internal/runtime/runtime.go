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
}

// EgressRule は実行基盤にある egress の rule。
type EgressRule struct {
	ID        string
	Decision  string // allow または deny
	Resources []string
}
