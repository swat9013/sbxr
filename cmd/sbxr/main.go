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
	runtime runtime.Runtime
	// userConfigPath は user 設定の path を返す。使う subcommand だけが呼ぶ。
	userConfigPath func() (string, error)
}

func main() {
	info, _ := debug.ReadBuildInfo() // 取れなければ nil が返る
	deps := dependencies{runtime: runtime.NewSbx(runtime.ExecSbx), userConfigPath: defaultUserConfigPath}
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

// defaultUserConfigPath は user 設定の path。$XDG_CONFIG_HOME が絶対 path ならその下、そうでなければ ~/.config の下
// (XDG Base Directory は相対 path の $XDG_CONFIG_HOME を無視するよう定めている)。
func defaultUserConfigPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "sbxr", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("user 設定の置き場を決められない: %w", err)
	}
	return filepath.Join(home, ".config", "sbxr", "config.yaml"), nil
}
