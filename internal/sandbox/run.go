package sandbox

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/shell"
)

// VM の agent user の home からの、run の script と、run の出力を足していく log の置き場。
const (
	runScriptRelPath = ".config/sbxr/run.sh"
	runLogRelPath    = ".local/state/sbxr/run.log"
)

// launchScript は $1 の script を、sbx exec が終わっても走り続けるよう新しい session で裏で始め、出力を $2 へ足していく。
// 始めたらすぐに終わる (script の終わりを待たない)。始められないこと (log を書けない・script を実行できない・setsid が無い) は
// 裏へ回す前に確かめて 0 以外で終わる (裏へ回した後の失敗は終了コードに出ない)。
const launchScript = `mkdir -p "$(dirname "$2")" && : >> "$2" || { echo "run の log $2 を書けない" >&2; exit 1; }
[ -x "$1" ] || { echo "$1 を実行できない" >&2; exit 1; }
command -v setsid > /dev/null || { echo "VM に setsid が無い" >&2; exit 1; }
nohup setsid "$1" >> "$2" 2>&1 < /dev/null &`

// startRun は作成時に確定した run を、VM 内の作業ツリーで裏で始める (decision/0028)。終わるのを待たない。
// run が無ければ何もしない。始めたら、始めたことと出力が残る VM 内の log の場所を示す。
// 始められなければ error (VM はそのまま残す)。
func startRun(ctx context.Context, rt runtime.Runtime, name, worktree string, commands []string, progress io.Writer) error {
	if len(commands) == 0 {
		return nil
	}
	v, err := openVM(ctx, rt, name)
	if err != nil {
		return fmt.Errorf("run を始められない: %w", err)
	}
	if err := v.writeFile(ctx, runScriptRelPath, []byte(runScript(worktree, commands)), 0o755); err != nil {
		return fmt.Errorf("run を始められない: run script を書けない: %w", err)
	}
	log := v.path(runLogRelPath)
	if _, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"bash", "-c", launchScript, "bash", v.path(runScriptRelPath), log}}); err != nil {
		return fmt.Errorf("run を始められない: %w", err)
	}
	logf(progress, "run: %d 件を VM 内の作業ツリー %s で裏で始めた (終わるのを待たない)。出力は VM の %s に残る (sbx exec %s -- tail -f %s)\n",
		len(commands), worktree, log, name, log)
	return nil
}

// runScript は run の各コマンドを作業ツリーで順に実行する script。1 件の失敗で止めず、各コマンドの終了を log に残す
// (sbxr は終わるのを待たないので、host からは結果が見えない)。
func runScript(worktree string, commands []string) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n# sbxr が書いた run。sbxr create と sbxr start の終わりに、裏で始める。\n")
	b.WriteString("echo \"=== sbxr run $(date -u +%Y-%m-%dT%H:%M:%SZ) ===\"\n")
	fmt.Fprintf(&b, "cd %s || exit 1\n", shell.Quote(worktree))
	for i, command := range commands {
		fmt.Fprintf(&b, "echo \"run[%d] start $(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)\"\n", i+1)
		fmt.Fprintf(&b, "bash -c %s < /dev/null; echo \"run[%d] exit=$? $(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)\"\n", shell.Quote(command), i+1)
	}
	return b.String()
}
