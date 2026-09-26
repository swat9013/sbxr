package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// CommandRunner は sbx を引数付きで実行し、stdout を返す (失敗したときも得られた分を返す)。stdin が nil なら何も渡さない (null device になる)。
// secret の値は argv に載せると process 一覧から見えるので、stdin で渡す。
type CommandRunner func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error)

// Sbx は Docker Sandboxes (sbx CLI) による Runtime の実装。
type Sbx struct {
	run CommandRunner
}

// NewSbx は run で sbx を呼ぶ Runtime を返す。本番では ExecSbx を渡す。
func NewSbx(run CommandRunner) *Sbx {
	return &Sbx{run: run}
}

// ExecSbx は PATH 上の sbx を子プロセスとして実行する。
func ExecSbx(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "sbx", args...)
	cmd.Stdin = stdin
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// 失敗しても stdout は返す (VM 内のコマンドが失敗の経緯を stdout に書くことがある)
		return out, fmt.Errorf("sbx %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
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
// editable でない rule (sbx 自身が管理する既定の rule) と sandbox スコープ rule は対象外 (ADR 0008)。
func (s *Sbx) ListGlobalEgressRules(ctx context.Context) ([]EgressRule, error) {
	out, err := s.run(ctx, nil, "policy", "ls", "--json")
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
		if r.Scope != "global" || r.ResourceType != "network" || !r.Editable {
			continue
		}
		decision := Decision(r.Decision)
		if decision != DecisionAllow && decision != DecisionDeny {
			return nil, fmt.Errorf("sbx の rule %s の decision %q を解釈できない", r.ID, r.Decision)
		}
		rules = append(rules, EgressRule{ID: r.ID, Decision: decision, Resources: r.Resources})
	}
	return rules, nil
}

// AllowGlobalEgress は sbx policy allow network で 1 resource の global rule を足す。
func (s *Sbx) AllowGlobalEgress(ctx context.Context, resource string) error {
	_, err := s.run(ctx, nil, "policy", "allow", "network", resource)
	return err
}

// RemoveGlobalEgressRule は sbx policy rm network --id で global rule を消す。
func (s *Sbx) RemoveGlobalEgressRule(ctx context.Context, id string) error {
	_, err := s.run(ctx, nil, "policy", "rm", "network", "--id", id)
	return err
}

// SetSandboxSecret は secret を sandbox スコープに置く。service があれば sbx 組み込み service の secret、
// 無ければ host 指定の custom secret (placeholder 注入) にする。値は stdin で渡す。
// sandbox スコープの secret は sandbox の作成前にも置け、作成時に VM の環境変数へ placeholder が入る (sbx 実測)。
func (s *Sbx) SetSandboxSecret(ctx context.Context, sandbox string, secret SandboxSecret) error {
	args := []string{"secret", "set", secret.Service, "--sandbox", sandbox}
	if secret.Service == "" {
		args = []string{"secret", "set-custom", "--sandbox", sandbox}
		for _, host := range secret.Hosts {
			args = append(args, "--host", host)
		}
		args = append(args, "--env", secret.Env)
	}
	// sbx は 1 行を値として読む (help の例は echo の出力を pipe する)
	_, err := s.run(ctx, strings.NewReader(secret.Value+"\n"), args...)
	return err
}

// SandboxStatus は sbx ls --json から sandbox の status を引く。
func (s *Sbx) SandboxStatus(ctx context.Context, sandbox string) (SandboxStatus, error) {
	out, err := s.run(ctx, nil, "ls", "--json")
	if err != nil {
		return "", err
	}
	var listing map[string]json.RawMessage
	if err := json.Unmarshal(out, &listing); err != nil {
		return "", fmt.Errorf("sbx ls --json の出力を読めない: %w", err)
	}
	raw, ok := listing["sandboxes"]
	if !ok {
		return "", fmt.Errorf("sbx ls --json の出力に sandboxes が無い")
	}
	var sandboxes []struct { // sandbox が 1 つも無ければ null でも空でもよい
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &sandboxes); err != nil {
		return "", fmt.Errorf("sbx ls --json の sandboxes を読めない: %w", err)
	}
	for _, sb := range sandboxes {
		if sb.Name == sandbox {
			return SandboxStatus(sb.Status), nil
		}
	}
	return SandboxAbsent, nil
}

// CreateEnvironment は sbx env create で作る。plan の承認は sbxr の確認関門が済ませているので --auto-approve を渡す。
func (s *Sbx) CreateEnvironment(ctx context.Context, envDir string) error {
	_, err := s.run(ctx, nil, "env", "create", "--auto-approve", envDir)
	return err
}

// RemoveEnvironment は sbx env rm で消す。sbx は stdin が端末でないと --force を要求し、--force は使用中の sandbox も消す。
// sandbox が無いときも「not found」を出して sandbox スコープの secret を消し、0 で終わる (実測。ADR 0006)。
func (s *Sbx) RemoveEnvironment(ctx context.Context, envDir string) error {
	_, err := s.run(ctx, nil, "env", "rm", "--force", envDir)
	return err
}

// StopSandbox は sbx stop で止める。
func (s *Sbx) StopSandbox(ctx context.Context, sandbox string) error {
	_, err := s.run(ctx, nil, "stop", sandbox)
	return err
}

// AllowSandboxEgress は sbx policy allow network --sandbox で sandbox スコープ rule を足す。
func (s *Sbx) AllowSandboxEgress(ctx context.Context, sandbox, resource string) error {
	_, err := s.run(ctx, nil, "policy", "allow", "network", "--sandbox", sandbox, resource)
	return err
}

// ExecInSandbox は sbx exec で VM 内のコマンドを実行する。Input があるときだけ -i で stdin を渡す。
func (s *Sbx) ExecInSandbox(ctx context.Context, sandbox string, command SandboxCommand) ([]byte, error) {
	args := []string{"exec"}
	var stdin io.Reader
	if command.Input != nil {
		args = append(args, "-i")
		stdin = bytes.NewReader(command.Input)
	}
	if command.Dir != "" {
		args = append(args, "-w", command.Dir)
	}
	args = append(append(args, sandbox, "--"), command.Args...)
	return s.run(ctx, stdin, args...)
}
