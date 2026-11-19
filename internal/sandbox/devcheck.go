package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/swat9013/sbxr/internal/runtime"
)

// devCheck は、VM の中で開発に使えることを確かめる 1 項目 (decision/0027)。
type devCheck struct {
	// command は VM の中で走らせるコマンド。0 で終われば成り立つ。
	command []string
	// unmet は成り立たなかったときに示す項目。
	unmet string
}

// devChecks は create と start の終わりに確かめる項目: agent (Claude Code) を起動できることと、Docker を使えること。
// Docker は daemon に繋がるところまで見る (docker info は daemon に繋がらなければ 0 以外で終える)。
var devChecks = []devCheck{
	{command: []string{"claude", "--version"}, unmet: "VM の中で agent (Claude Code) を起動できない"},
	{command: []string{"docker", "info", "--format", "{{.ServerVersion}}"}, unmet: "VM の中で Docker を使えない"},
}

// checkDevTools は稼働中の sandbox VM の中で、agent と Docker が使えることを確かめる。
// 1 項目が成り立たなくても残りを確かめ、成り立たなかった項目をすべて error で示す。
func checkDevTools(ctx context.Context, rt runtime.Runtime, name string, progress io.Writer) error {
	var unmet []error
	for _, check := range devChecks {
		if _, err := rt.ExecInSandbox(ctx, name, runtime.SandboxCommand{Args: check.command}); err != nil {
			unmet = append(unmet, fmt.Errorf("%s: %w", check.unmet, err))
		}
	}
	if len(unmet) > 0 {
		return errors.Join(unmet...)
	}
	logf(progress, "VM の中で agent (Claude Code) を起動でき、Docker を使えることを確かめた\n")
	return nil
}
