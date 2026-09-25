package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// CommandRunner は sbx を引数付きで実行し、stdout を返す。
type CommandRunner func(ctx context.Context, args ...string) ([]byte, error)

// Sbx は Docker Sandboxes (sbx CLI) による Runtime の実装。
type Sbx struct {
	run CommandRunner
}

// NewSbx は run で sbx を呼ぶ Runtime を返す。本番では ExecSbx を渡す。
func NewSbx(run CommandRunner) *Sbx {
	return &Sbx{run: run}
}

// ExecSbx は PATH 上の sbx を子プロセスとして実行する。stdin は渡さない (null device になる)。
func ExecSbx(ctx context.Context, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "sbx", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sbx %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

var _ Runtime = (*Sbx)(nil)

// sbxRule は sbx policy ls --json の rule のうち、収束の対象を決めるのに読む field。
type sbxRule struct {
	ID           string   `json:"id"`
	Scope        string   `json:"scope"`
	ResourceType string   `json:"resource_type"`
	Decision     string   `json:"decision"`
	Resources    []string `json:"resources"`
	Editable     bool     `json:"editable"`
}

// ListGlobalEgressRules は scope=global・resource_type=network・editable=true の rule を返す。
// editable でない rule (sbx 自身が管理する既定の rule) と sandbox スコープ rule は対象外。
func (s *Sbx) ListGlobalEgressRules(ctx context.Context) ([]EgressRule, error) {
	out, err := s.run(ctx, "policy", "ls", "--json")
	if err != nil {
		return nil, err
	}
	var listing struct {
		Rules *[]sbxRule `json:"rules"`
	}
	if err := json.Unmarshal(out, &listing); err != nil {
		return nil, fmt.Errorf("sbx policy ls --json の出力を読めない: %w", err)
	}
	if listing.Rules == nil {
		return nil, fmt.Errorf("sbx policy ls --json の出力に rules が無い")
	}
	var rules []EgressRule
	for _, r := range *listing.Rules {
		if r.Scope == "global" && r.ResourceType == "network" && r.Editable {
			rules = append(rules, EgressRule{ID: r.ID, Decision: Decision(r.Decision), Resources: r.Resources})
		}
	}
	return rules, nil
}

// AllowGlobalEgress は sbx policy allow network で 1 resource の global rule を足す。
func (s *Sbx) AllowGlobalEgress(ctx context.Context, resource string) error {
	_, err := s.run(ctx, "policy", "allow", "network", resource)
	return err
}

// RemoveGlobalEgressRule は sbx policy rm network --id で global rule を消す。
func (s *Sbx) RemoveGlobalEgressRule(ctx context.Context, id string) error {
	_, err := s.run(ctx, "policy", "rm", "network", "--id", id)
	return err
}
