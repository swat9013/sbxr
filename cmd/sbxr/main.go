// Command sbxr は repo を宣言 1 枚で AI coding agent 用の sandbox VM にする CLI。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/sandbox"
)

// version は release build で -ldflags "-X main.version=..." により埋め込まれる。
var version string

// dependencies は subcommand が使う外部との境界。test では stub に差し替える。
type dependencies struct {
	runtime runtime.Runtime
	// userConfigPath は user 設定の path を返す。使う subcommand だけが呼ぶ。
	userConfigPath func() (string, error)
	// secretFilePath は secret ファイルの path を返す。使う subcommand だけが呼ぶ。
	secretFilePath func() (string, error)
	// githubAPI は GitHub API の root URL。
	githubAPI string
	prompter  prompter
	// places は状態ディレクトリ・cache clone・user 設定の置き場を返す。
	places func() (sandbox.Places, error)
	clone  sandbox.Cloner
}

func main() {
	info, _ := debug.ReadBuildInfo() // 取れなければ nil が返る
	deps := dependencies{
		runtime:        runtime.NewSbx(runtime.ExecSbx),
		userConfigPath: defaultUserConfigPath,
		secretFilePath: defaultSecretFilePath,
		githubAPI:      "https://api.github.com",
		prompter:       newTerminalPrompter(),
		places:         defaultPlaces,
		clone:          sandbox.ExecClone,
	}
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
	root.AddCommand(newPlanCmd(deps), newCreateCmd(deps), newDestroyCmd(deps), newStopCmd(deps), newPolicyCmd(deps), newSecretCmd(deps))
	return root
}

// defaultUserConfigPath は user 設定の path (~/.config/sbxr/config.yaml。ADR 0004)。
func defaultUserConfigPath() (string, error) {
	return configFilePath("user 設定", "config.yaml")
}

// defaultSecretFilePath は secret ファイルの path (~/.config/sbxr/secrets.env。ADR 0002)。
func defaultSecretFilePath() (string, error) {
	return configFilePath("secret ファイル", "secrets.env")
}

func configFilePath(what, name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%sの置き場を決められない: %w", what, err)
	}
	return filepath.Join(home, ".config", "sbxr", name), nil
}
