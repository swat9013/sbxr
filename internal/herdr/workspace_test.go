package herdr_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/herdr"
	"github.com/swat9013/sbxr/internal/herdr/servertest"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/inmemory"
)

// worktree は空白を含む VM 内の作業ツリー (復旧手順で 1 つの引数として示されるかを見る)。
const worktree = "/Users/u/My Projects/app"

const quotedWorktree = "'" + worktree + "'"

// appWithServer は稼働中の sandbox VM app と、その中で herdr machine add が起動した herdr server。
func appWithServer() (*inmemory.Runtime, *servertest.Server) {
	rt := runningApp()
	server := servertest.NewServer()
	rt.Respond = func(_ string, command runtime.SandboxCommand) ([]byte, error) {
		out, _, err := server.Answer(command.Args)
		return out, err
	}
	return rt, server
}

func startAppAtWorktree(rt *inmemory.Runtime) error {
	return herdr.StartAtWorktree(context.Background(), rt, "app", worktree)
}

func TestStartAtWorktreeLeavesOnlyAWorkspaceThatStartsAtTheWorktree(t *testing.T) {
	rt, server := appWithServer()

	err := startAppAtWorktree(rt)

	if err != nil || len(server.Workspaces) != 1 || server.Workspaces[0].Cwd != worktree {
		t.Errorf("StartAtWorktree() = %v, workspaces = %v, want only one at %s", err, server.Workspaces, worktree)
	}
}

func TestStartAtWorktreeOpensTheWorktreeWorkspaceBeforeClosingTheStartupOne(t *testing.T) {
	rt, _ := appWithServer()

	_ = startAppAtWorktree(rt)

	subcommand := func(name string) int {
		return slices.IndexFunc(rt.Commands, func(c inmemory.Command) bool {
			return len(c.Args) >= 3 && slices.Equal(c.Args[:3], []string{"herdr", "workspace", name})
		})
	}
	if created, closed := subcommand("create"), subcommand("close"); created < 0 || closed < created {
		t.Errorf("VM commands = %v, want the workspace opened before the startup one is closed", rt.Commands)
	}
}

func TestStartAtWorktreeKeepsTheStartupWorkspaceWhenHerdrStartsThePaneElsewhere(t *testing.T) {
	rt, server := appWithServer()
	server.IgnoreCwd = true

	_ = startAppAtWorktree(rt)

	if len(server.Workspaces) != 2 || server.Workspaces[0].ID != "w1" {
		t.Errorf("workspaces = %v, want the startup workspace kept", server.Workspaces)
	}
}

func TestStartAtWorktreeNamesWhereThePaneStartedWhenItIsNotTheWorktree(t *testing.T) {
	rt, server := appWithServer()
	server.IgnoreCwd = true

	err := startAppAtWorktree(rt)

	if err == nil || !strings.Contains(err.Error(), servertest.StartupCwd) {
		t.Errorf("StartAtWorktree() = %v, want the cwd the pane started at", err)
	}
}

func TestStartAtWorktreeShowsHowToCloseTheWorkspaceThatStartedElsewhere(t *testing.T) {
	rt, server := appWithServer()
	server.IgnoreCwd = true

	err := startAppAtWorktree(rt)

	var start *herdr.WorktreeStartError
	if !errors.As(err, &start) || !strings.Contains(start.Recovery, "herdr workspace close w2") {
		t.Errorf("StartAtWorktree() = %v, want how to close the workspace that started elsewhere", err)
	}
}

func TestStartAtWorktreeShowsHowToOpenAndCloseByHandWhenTheWorkspaceCannotBeOpened(t *testing.T) {
	rt, server := appWithServer()
	server.FailCreate = true

	err := startAppAtWorktree(rt)

	var start *herdr.WorktreeStartError
	if !errors.As(err, &start) || !strings.Contains(start.Recovery, "herdr workspace create --cwd "+quotedWorktree+" --focus && herdr workspace close w1") {
		t.Errorf("StartAtWorktree() = %v, want how to open the workspace and close the startup one by hand", err)
	}
}

func TestStartAtWorktreeShowsHowToOpenAndCloseByHandWhenTheWorkspacesCannotBeListed(t *testing.T) {
	rt, server := appWithServer()
	server.FailList = true

	err := startAppAtWorktree(rt)

	var start *herdr.WorktreeStartError
	if !errors.As(err, &start) || !strings.Contains(start.Recovery, "herdr workspace create --cwd "+quotedWorktree+" --focus") ||
		!strings.Contains(start.Recovery, "herdr workspace close <id>") {
		t.Errorf("StartAtWorktree() = %v, want how to open the workspace and close the others by hand", err)
	}
}

func TestStartAtWorktreeShowsHowToCheckBeforeOpeningAgainWhenItsOutputCannotBeRead(t *testing.T) {
	rt, server := appWithServer()
	server.GarbleCreate = true

	err := startAppAtWorktree(rt)

	var start *herdr.WorktreeStartError
	if !errors.As(err, &start) || !strings.HasPrefix(start.Recovery, "VM app の中で herdr workspace list を実行し") {
		t.Errorf("StartAtWorktree() = %v, want the workspaces checked before opening one again", err)
	}
}

func TestStartAtWorktreeClosesTheOtherStartupWorkspacesWhenOneCannotBeClosed(t *testing.T) {
	rt, server := appWithServer()
	server.Workspaces = append(server.Workspaces, servertest.Workspace{ID: "w9", Cwd: servertest.StartupCwd})
	server.FailClose = []string{"w1"}

	_ = startAppAtWorktree(rt)

	if slices.ContainsFunc(server.Workspaces, func(w servertest.Workspace) bool { return w.ID == "w9" }) {
		t.Errorf("workspaces = %v, want w9 closed", server.Workspaces)
	}
}

func TestStartAtWorktreeShowsHowToCloseOnlyTheWorkspacesItCouldNot(t *testing.T) {
	rt, server := appWithServer()
	server.Workspaces = append(server.Workspaces, servertest.Workspace{ID: "w9", Cwd: servertest.StartupCwd})
	server.FailClose = []string{"w1"}

	err := startAppAtWorktree(rt)

	var start *herdr.WorktreeStartError
	if !errors.As(err, &start) || start.Recovery != "VM app の中で herdr workspace close w1 を実行する" {
		t.Errorf("StartAtWorktree() = %v, want how to close only w1 by hand", err)
	}
}
