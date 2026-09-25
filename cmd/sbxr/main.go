// Command sbxr は repo を宣言 1 枚で AI coding agent 用の sandbox VM にする CLI。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/swat9013/sbxr/internal/runtime"
)

// version は release build で -ldflags "-X main.version=..." により埋め込まれる。
var version string

// dependencies は subcommand が使う外部との境界。test では stub に差し替える。
type dependencies struct {
	runtime        runtime.Runtime
	userConfigPath string
}

func main() {
	info, _ := debug.ReadBuildInfo() // 取れなければ nil が返る
	userConfigPath, err := defaultUserConfigPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	deps := dependencies{runtime: runtime.NewSbx(runtime.ExecSbx), userConfigPath: userConfigPath}
	if err := newRootCmd(resolveVersion(version, info), deps).Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd(version string, deps dependencies) *cobra.Command {
	root := &cobra.Command{
		Use:     "sbxr",
		Short:   "repo を宣言 1 枚で AI coding agent 用の sandbox VM にする",
		Version: version,
	}
	root.AddCommand(newPolicyCmd(deps))
	return root
}

// defaultUserConfigPath は user 設定の path。$XDG_CONFIG_HOME があればその下、無ければ ~/.config の下。
func defaultUserConfigPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "sbxr", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user 設定の置き場を決められない: %w", err)
	}
	return filepath.Join(home, ".config", "sbxr", "config.yaml"), nil
}
