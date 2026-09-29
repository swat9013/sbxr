package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/swat9013/sbxr/internal/runtime"
)

// WorktreeStartError は、herdr machine を登録した後で、VM 内の herdr の最初の workspace を VM 内の作業ツリーから
// 始め直せなかったときの error。sandbox VM と herdr machine の登録は済んでいる。
type WorktreeStartError struct {
	Err error
	// Recovery は VM 内で手で始め直すコマンド。
	Recovery string
}

func (e *WorktreeStartError) Error() string {
	return fmt.Sprintf("herdr の最初の workspace を VM 内の作業ツリーから始められない: %v", e.Err)
}
func (e *WorktreeStartError) Unwrap() error { return e.Err }

// StartAtWorktree は、登録で起動した VM 内の herdr server の最初の workspace を、VM 内の作業ツリーから始め直す
// (decision/0015)。server は起動時の workspace を自身の cwd (ssh の既定の cwd) に作り、new_cwd はそれに効かないので、
// 作業ツリーを cwd にした workspace を開いてから、それより前の workspace を閉じる (workspace が 0 件になる間を作らない)。
// 起動時の workspace は、server が socket で受け付けを始める前に作られる (実測の server の log の順) ので、
// herdr machine add が返った後の一覧には必ず載っている。
func (r Registry) StartAtWorktree(ctx context.Context, sandbox, worktree string) error {
	open := fmt.Sprintf("VM %s の中で herdr workspace create --cwd %s --focus を実行する", sandbox, shellQuote(worktree))
	before, err := r.workspaces(ctx, sandbox)
	if err != nil {
		return &WorktreeStartError{Err: err, Recovery: open}
	}
	out, err := r.runInVM(ctx, sandbox, "workspace", "create", "--cwd", worktree, "--focus")
	if err != nil {
		return &WorktreeStartError{Err: err, Recovery: open}
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
		return &WorktreeStartError{Err: fmt.Errorf("herdr workspace create の出力を読めない: %w", err), Recovery: open}
	}
	// 版の違いで --cwd が効かなくても herdr は成功で返しうるので、始まった場所を確かめる。
	// 起動時の workspace は閉じずに残す (作業ツリーで始まる workspace が無いまま 0 件にしない)
	if got := created.Result.RootPane.Cwd; got != worktree {
		return &WorktreeStartError{
			Err: fmt.Errorf("herdr workspace create --cwd %s の pane が %q で始まった", worktree, got),
			Recovery: fmt.Sprintf("VM %s の herdr の版が --cwd を扱えるかを確かめ、開いた workspace %s を herdr workspace close %s で閉じる",
				sandbox, created.Result.Workspace.ID, created.Result.Workspace.ID),
		}
	}
	// 1 つ閉じられなくても残りは閉じ、閉じられなかったものをまとめて示す
	var unclosed []string
	var closeErr error
	for _, id := range before {
		if _, err := r.runInVM(ctx, sandbox, "workspace", "close", id); err != nil {
			unclosed = append(unclosed, id)
			closeErr = errors.Join(closeErr, err)
		}
	}
	if len(unclosed) > 0 {
		var commands []string
		for _, id := range unclosed {
			commands = append(commands, "herdr workspace close "+id)
		}
		return &WorktreeStartError{Err: closeErr, Recovery: fmt.Sprintf("VM %s の中で %s を実行する", sandbox, strings.Join(commands, "; "))}
	}
	return nil
}

// shellQuote は path を、復旧手順として貼り付けても 1 つの引数になる形で示す。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// workspaces は VM 内の herdr server の workspace の id を返す。
func (r Registry) workspaces(ctx context.Context, sandbox string) ([]string, error) {
	out, err := r.runInVM(ctx, sandbox, "workspace", "list")
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
		return nil, fmt.Errorf("herdr workspace list の出力を読めない: %w", err)
	}
	ids := make([]string, 0, len(listed.Result.Workspaces))
	for _, w := range listed.Result.Workspaces {
		ids = append(ids, w.ID)
	}
	return ids, nil
}

// runInVM は VM 内の herdr CLI を実行する (VM 内の server に socket で繋ぐ)。
func (r Registry) runInVM(ctx context.Context, sandbox string, args ...string) ([]byte, error) {
	out, err := r.VM.ExecInSandbox(ctx, sandbox, runtime.SandboxCommand{Args: append([]string{"herdr"}, args...)})
	if err != nil {
		return nil, fmt.Errorf("VM 内の herdr %v: %w", args, err)
	}
	return out, nil
}
