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
	Enable(ctx context.Context, id string) error
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

// herdr の各操作の引数。CLI の実行と、利用者へ見せる復旧手順の両方がここから作る。

func addArgs(target, label string) []string {
	return []string{"machine", "add", target, "--label", label}
}
func enableArgs(id string) []string  { return []string{"machine", "enable", id} }
func disableArgs(id string) []string { return []string{"machine", "disable", id} }
func removeArgs(id string) []string  { return []string{"machine", "remove", id} }

func command(args []string) string { return "herdr " + strings.Join(args, " ") }

// AddCommand は target を登録する herdr のコマンド (復旧手順として見せる)。
func AddCommand(target, label string) string { return command(addArgs(target, label)) }

// EnableCommand は machine を有効に戻す herdr のコマンド。
func EnableCommand(id string) string { return command(enableArgs(id)) }

// RemoveCommand は machine を解除する herdr のコマンド。
func RemoveCommand(id string) string { return command(removeArgs(id)) }

// ParseMachines は herdr machine list --json の出力を読む。
func ParseMachines(out []byte) ([]Machine, error) {
	var machines []Machine
	if err := json.Unmarshal(out, &machines); err != nil {
		return nil, fmt.Errorf("herdr machine list --json の出力を読めない: %w", err)
	}
	return machines, nil
}

// CLI は PATH 上の herdr を子プロセスとして呼ぶ Client。stdin は渡さない (null device になる)。
type CLI struct{}

var _ Client = CLI{}

// Available は PATH に herdr があるかを確かめる。
func (CLI) Available() error {
	if _, err := exec.LookPath("herdr"); err != nil {
		return fmt.Errorf("host に herdr が無い: %w", err)
	}
	return nil
}

func (CLI) List(ctx context.Context) ([]Machine, error) {
	out, err := run(ctx, []string{"machine", "list", "--json"})
	if err != nil {
		return nil, err
	}
	return ParseMachines(out)
}

func (CLI) Add(ctx context.Context, target, label string) error {
	_, err := run(ctx, addArgs(target, label))
	return err
}

func (CLI) Enable(ctx context.Context, id string) error {
	_, err := run(ctx, enableArgs(id))
	return err
}

func (CLI) Disable(ctx context.Context, id string) error {
	_, err := run(ctx, disableArgs(id))
	return err
}

func (CLI) Remove(ctx context.Context, id string) error {
	_, err := run(ctx, removeArgs(id))
	return err
}

func run(ctx context.Context, args []string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "herdr", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("%s: %w: %s", command(args), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
