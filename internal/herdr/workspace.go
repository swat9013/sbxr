package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/swat9013/sbxr/internal/runtime"
)

// WorktreeStartError は、herdr machine を登録した後で、VM 内の herdr の最初の workspace を VM 内の作業ツリーで
// 開き直せなかったときの error。RegistrationError と分けるのは、済んだことが違うから: こちらは herdr machine の登録まで
// 済んでいて、復旧は VM 内の workspace の操作だけになる (呼び出し側は済んだことを利用者に示す)。
type WorktreeStartError struct {
	Err error
	// Recovery は VM 内で手で開き直す手順。
	Recovery string
}

func (e *WorktreeStartError) Error() string {
	return fmt.Sprintf("herdr の最初の workspace を VM 内の作業ツリーで開き直せない: %v", e.Err)
}
func (e *WorktreeStartError) Unwrap() error { return e.Err }

// StartAtWorktree は、herdr machine の登録で起動した VM 内の herdr server の最初の workspace を、VM 内の作業ツリーで
// 開き直す (decision/0015)。server は起動時の workspace を自身の cwd (ssh の既定の cwd) に作り、new_cwd はそれに効かないので、
// 作業ツリーを cwd にした workspace を開いてから、それより前の workspace を閉じる (workspace が 0 件になる間を作らない)。
// 起動時の workspace は、server が socket で受け付けを始める前に作られる (実測の server の log の順) ので、
// herdr machine add が返った後の一覧には必ず載っている。
func StartAtWorktree(ctx context.Context, vm VM, sandbox, worktree string) error {
	create := workspaceCreateArgs(worktree)
	before, err := workspaces(ctx, vm, sandbox)
	if err != nil {
		return &WorktreeStartError{Err: err, Recovery: fmt.Sprintf("VM %s の中で %s を実行し、%s に出る他の workspace を %s で閉じる",
			sandbox, command(create), command(workspaceListArgs()), command(workspaceCloseArgs("<id>")))}
	}
	closeBefore := closeCommands(before)
	out, err := runInVM(ctx, vm, sandbox, create)
	if err != nil {
		return &WorktreeStartError{Err: err, Recovery: fmt.Sprintf("VM %s の中で %s", sandbox, strings.Join(append([]string{command(create)}, closeBefore...), "; "))}
	}
	var created struct {
		Result struct {
			Workspace struct {
				ID string `json:"workspace_id"`
			} `json:"workspace"`
			RootPane struct {
				Cwd string `json:"cwd"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &created); err != nil {
		// workspace は開けている見込みが高いので、開き直させる前に一覧で確かめさせる
		return &WorktreeStartError{
			Err: fmt.Errorf("herdr workspace create の出力 %q を読めない: %w", out, err),
			Recovery: fmt.Sprintf("VM %s の中で %s を実行し、作業ツリーの workspace が無ければ %s、あれば %s",
				sandbox, command(workspaceListArgs()), command(create), strings.Join(closeBefore, "; ")),
		}
	}
	// 版の違いで --cwd が効かなくても herdr は成功で返しうるので、始まった場所を確かめる (path は文字列で比べる)。
	// 起動時の workspace は閉じずに残す (作業ツリーで始まる workspace が無いまま 0 件にしない)
	if got := created.Result.RootPane.Cwd; got != worktree {
		return &WorktreeStartError{
			Err: fmt.Errorf("%s の pane が %q で始まった", command(create), got),
			Recovery: fmt.Sprintf("VM %s の herdr の版が --cwd を扱えるかを確かめ、開いた workspace を %s で閉じる",
				sandbox, command(workspaceCloseArgs(created.Result.Workspace.ID))),
		}
	}
	// 1 つ閉じられなくても残りは閉じ、閉じられなかったものをまとめて示す
	var unclosed []string
	var closeErr error
	for _, id := range before {
		if _, err := runInVM(ctx, vm, sandbox, workspaceCloseArgs(id)); err != nil {
			unclosed = append(unclosed, id)
			closeErr = errors.Join(closeErr, err)
		}
	}
	if len(unclosed) > 0 {
		return &WorktreeStartError{Err: closeErr, Recovery: fmt.Sprintf("VM %s の中で %s", sandbox, strings.Join(closeCommands(unclosed), "; "))}
	}
	return nil
}

// closeCommands は ids の workspace を閉じる herdr のコマンド (復旧手順として見せる)。
func closeCommands(ids []string) []string {
	commands := make([]string, 0, len(ids))
	for _, id := range ids {
		commands = append(commands, command(workspaceCloseArgs(id)))
	}
	return commands
}

// workspaces は VM 内の herdr server の workspace の id を返す。
func workspaces(ctx context.Context, vm VM, sandbox string) ([]string, error) {
	out, err := runInVM(ctx, vm, sandbox, workspaceListArgs())
	if err != nil {
		return nil, err
	}
	var listed struct {
		Result struct {
			Workspaces []struct {
				ID string `json:"workspace_id"`
			} `json:"workspaces"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		return nil, fmt.Errorf("%s の出力 %q を読めない: %w", command(workspaceListArgs()), out, err)
	}
	ids := make([]string, 0, len(listed.Result.Workspaces))
	for _, w := range listed.Result.Workspaces {
		ids = append(ids, w.ID)
	}
	return ids, nil
}

// runInVM は VM 内の herdr CLI を実行する (VM 内の server に socket で繋ぐ)。
func runInVM(ctx context.Context, vm VM, sandbox string, args []string) ([]byte, error) {
	out, err := vm.ExecInSandbox(ctx, sandbox, runtime.SandboxCommand{Args: append([]string{"herdr"}, args...)})
	if err != nil {
		return nil, fmt.Errorf("VM 内の %s: %w", command(args), err)
	}
	return out, nil
}
