// Package herdr は host の herdr CLI で herdr machine を扱う (ADR 0007)。sbx の lifecycle からではなく sbxr 本体が呼ぶ。
package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// Machine は host の herdr に保存された SSH machine。
type Machine struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Target  string `json:"target"`
	Enabled bool   `json:"enabled"`
}

// Client は sbxr が host の herdr に求める操作。
type Client interface {
	// Available は host に herdr があるかを確かめる。無ければ error。
	Available() error
	List(ctx context.Context) ([]Machine, error)
	// Add は target (SSH の宛先) を label で登録する。
	Add(ctx context.Context, target, label string) error
	Disable(ctx context.Context, id string) error
	Remove(ctx context.Context, id string) error
}

// Target は sandbox VM の herdr machine の SSH の宛先 (sbx が ~/.ssh/config に置く <name>.sbx)。
func Target(sandbox string) string {
	return sandbox + ".sbx"
}

// Find は target の machine を返す。無ければ ok が false。
func Find(machines []Machine, target string) (Machine, bool) {
	for _, machine := range machines {
		if machine.Target == target {
			return machine, true
		}
	}
	return Machine{}, false
}

// AddCommand は target を登録する herdr のコマンド (復旧手順として見せる)。
func AddCommand(target, label string) string {
	return fmt.Sprintf("herdr machine add %s --label %s", target, label)
}

// EnableCommand は machine を有効に戻す herdr のコマンド。
func EnableCommand(id string) string {
	return "herdr machine enable " + id
}

// CLI は PATH 上の herdr を子プロセスとして呼ぶ Client。stdin は渡さない (null device になる)。
type CLI struct{}

var _ Client = CLI{}

// Available は PATH に herdr があるかを確かめる。
func (CLI) Available() error {
	if _, err := exec.LookPath("herdr"); err != nil {
		return fmt.Errorf("herdr 連携が有効だが、host に herdr が無い (PATH に herdr を入れるか、user 設定で herdr.enabled: false にする): %w", err)
	}
	return nil
}

func (CLI) List(ctx context.Context) ([]Machine, error) {
	out, err := run(ctx, "herdr", "machine", "list", "--json")
	if err != nil {
		return nil, err
	}
	var machines []Machine
	if err := json.Unmarshal(out, &machines); err != nil {
		return nil, fmt.Errorf("herdr machine list --json の出力を読めない: %w", err)
	}
	return machines, nil
}

// Add は host の ssh が target の ProxyCommand を持つか (sbx の ssh 設定が済んでいるか) を確かめてから登録する。
func (CLI) Add(ctx context.Context, target, label string) error {
	out, err := run(ctx, "ssh", "-G", target)
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(string(out)), "\nproxycommand ") {
		return fmt.Errorf("host の ssh が %s の ProxyCommand を持たない (sbx の ssh 設定が済んでいるか確かめる)", target)
	}
	_, err = run(ctx, "herdr", "machine", "add", target, "--label", label)
	return err
}

func (CLI) Disable(ctx context.Context, id string) error {
	_, err := run(ctx, "herdr", "machine", "disable", id)
	return err
}

func (CLI) Remove(ctx context.Context, id string) error {
	_, err := run(ctx, "herdr", "machine", "remove", id)
	return err
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
