// Command sbxr は repo を宣言 1 枚で AI coding agent 用の sandbox VM にする CLI。
package main

import (
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version は release build で -ldflags "-X main.version=..." により埋め込まれる。
var version string

func main() {
	info, _ := debug.ReadBuildInfo() // 取れなければ nil が返る
	if err := newRootCmd(resolveVersion(version, info)).Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:     "sbxr",
		Short:   "repo を宣言 1 枚で AI coding agent 用の sandbox VM にする",
		Version: version,
	}
}
