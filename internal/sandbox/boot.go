package sandbox

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/swat9013/sbxr/internal/runtime"
)

// BootScriptRelPath は VM の agent user の home からの boot の置き場。埋め込みの kit sbxr-boot が起動ごとに実行する
// (kit 側の path との一致は test が確かめる)。
const BootScriptRelPath = ".config/sbxr/boot.sh"

// aptWait は init の前に VM 内の apt-get が終わるのを待つ上限と間隔。sleep は test が差し替える。
var aptWait = struct {
	budget, interval time.Duration
	sleep            func(time.Duration)
}{300 * time.Second, 2 * time.Second, time.Sleep}

// runInit は init を VM 内の repo root で 1 件ずつ実行する。1 件以上あるときだけ、先に VM 内の apt-get が終わるのを待つ
// (起動直後に sbx が background で走らせる apt と、init の apt が lock で衝突するのを避ける)。
func runInit(ctx context.Context, v vm, repo string, commands []string, progress io.Writer) error {
	if len(commands) == 0 {
		return nil
	}
	finished, err := waitForApt(ctx, v)
	if err != nil {
		return err
	}
	if !finished {
		logf(progress, "init: 警告 VM 内の apt-get が %s 経っても終わらない (init の apt が lock で失敗しうる)\n", aptWait.budget)
	}
	for i, command := range commands {
		logf(progress, "init[%d]: %s\n", i+1, command)
		out, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"bash", "-c", command}, Dir: repo})
		logf(progress, "%s", out)
		if err != nil {
			return fmt.Errorf("init[%d] (%s): %w", i+1, command, err)
		}
	}
	return nil
}

// waitForApt は VM 内の apt-get が終わるまで待つ。上限までに終われば true。
// pgrep は apt-get が無いと 0 以外で終わるので、error を「動いていない」と読む。ctx が切れたら error を返す。
func waitForApt(ctx context.Context, v vm) (bool, error) {
	for waited := time.Duration(0); waited < aptWait.budget; waited += aptWait.interval {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if _, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"pgrep", "-x", "apt-get"}}); err != nil {
			return true, ctx.Err()
		}
		aptWait.sleep(aptWait.interval)
	}
	return false, nil
}

// runBoot は boot を VM の boot script に書き、create 時の 1 回を実行する。2 回目以降の起動では kit sbxr-boot が実行する。
// 起動ごとに宣言を読み直さず、作成時に確定した内容を再生する。
func runBoot(ctx context.Context, v vm, repo string, commands []string, progress io.Writer) error {
	if len(commands) == 0 {
		return nil
	}
	if err := v.writeFile(ctx, BootScriptRelPath, []byte(bootScript(repo, commands)), 0o755); err != nil {
		return fmt.Errorf("boot script を書けない: %w", err)
	}
	logf(progress, "boot: %d 件を VM の ~/%s に書いた (起動ごとに実行する)\n", len(commands), BootScriptRelPath)
	out, err := v.run(ctx, runtime.SandboxCommand{Args: []string{v.path(BootScriptRelPath)}})
	logf(progress, "%s", out) // 失敗したときも、どの entry が失敗したか (boot[N] fail) を見せる
	return err
}

// bootScript は boot の各コマンドを repo root で順に実行する script。1 件の失敗で止めず、最後に失敗を返す
// (起動ごとの実行では host から見えないので、どの entry が失敗したかを log に残す)。
func bootScript(repo string, commands []string) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n# sbxr が create 時に書いた boot。kit sbxr-boot が sandbox VM の起動ごとに実行する。\n")
	fmt.Fprintf(&b, "rc=0\ncd %s || exit 1\n", shellQuote(repo))
	for i, command := range commands {
		fmt.Fprintf(&b, "echo 'boot[%d]: start'\n", i+1)
		fmt.Fprintf(&b, "bash -c %s < /dev/null || { echo \"boot[%d] fail exit=$?\"; rc=1; }\n", shellQuote(command), i+1)
	}
	b.WriteString("exit \"$rc\"\n")
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
