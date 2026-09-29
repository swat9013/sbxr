package herdr_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/herdr"
	"github.com/swat9013/sbxr/internal/herdr/herdrtest"
	"github.com/swat9013/sbxr/internal/herdr/servertest"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/inmemory"
)

const worktree = "/Users/u/src/app"

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

func TestStartAtWorktreeLeavesOnlyAWorkspaceThatStartsAtTheWorktree(t *testing.T) {
	rt, server := appWithServer()

	err := herdr.Registry{Client: &herdrtest.Fake{}, VM: rt}.StartAtWorktree(context.Background(), "app", worktree)

	if err != nil || len(server.Workspaces) != 1 || server.Workspaces[0].Cwd != worktree {
		t.Errorf("StartAtWorktree() = %v, workspaces = %v, want only one at %s", err, server.Workspaces, worktree)
	}
}

func TestStartAtWorktreeOpensTheWorktreeWorkspaceBeforeClosingTheStartupOne(t *testing.T) {
	rt, _ := appWithServer()

	_ = herdr.Registry{Client: &herdrtest.Fake{}, VM: rt}.StartAtWorktree(context.Background(), "app", worktree)

	var herdrCommands []string
	for _, command := range rt.Commands {
		herdrCommands = append(herdrCommands, strings.Join(command.Args, " "))
	}
	want := "herdr workspace list, herdr workspace create --cwd " + worktree + " --focus, herdr workspace close w1"
	if strings.Join(herdrCommands, ", ") != want {
		t.Errorf("herdr commands in the VM = %v, want the workspace opened before the startup one is closed", herdrCommands)
	}
}

func TestStartAtWorktreeFailsWhenHerdrStartsThePaneElsewhere(t *testing.T) {
	rt, server := appWithServer()
	server.IgnoreCwd = true

	err := herdr.Registry{Client: &herdrtest.Fake{}, VM: rt}.StartAtWorktree(context.Background(), "app", worktree)

	var start *herdr.WorktreeStartError
	if !errors.As(err, &start) || !strings.Contains(err.Error(), servertest.StartupCwd) || len(server.Workspaces) != 2 {
		t.Errorf("StartAtWorktree() = %v, workspaces = %v, want the wrong cwd named and the startup workspace kept", err, server.Workspaces)
	}
	if start != nil && !strings.Contains(start.Recovery, "herdr workspace close w2") {
		t.Errorf("recovery = %q, want how to close the workspace that started elsewhere", start.Recovery)
	}
}

func TestStartAtWorktreeShowsHowToOpenTheWorkspaceByHandWhenItCannotBeCreated(t *testing.T) {
	rt, server := appWithServer()
	server.FailCreate = true

	err := herdr.Registry{Client: &herdrtest.Fake{}, VM: rt}.StartAtWorktree(context.Background(), "app", worktree)

	var start *herdr.WorktreeStartError
	if !errors.As(err, &start) || !strings.Contains(start.Recovery, "herdr workspace create --cwd '"+worktree+"' --focus") {
		t.Errorf("StartAtWorktree() = %v, want how to open the workspace by hand", err)
	}
}

func TestStartAtWorktreeClosesTheRestAndShowsHowToCloseTheOnesItCouldNot(t *testing.T) {
	rt, server := appWithServer()
	server.Workspaces = append(server.Workspaces, servertest.Workspace{ID: "w9", Cwd: servertest.StartupCwd})
	server.FailClose = []string{"w1"}

	err := herdr.Registry{Client: &herdrtest.Fake{}, VM: rt}.StartAtWorktree(context.Background(), "app", worktree)

	var start *herdr.WorktreeStartError
	if !errors.As(err, &start) || !strings.Contains(start.Recovery, "herdr workspace close w1") || strings.Contains(start.Recovery, "w9") {
		t.Errorf("StartAtWorktree() = %v, want how to close only w1 by hand", err)
	}
	if len(server.Workspaces) != 2 || server.Workspaces[0].ID != "w1" {
		t.Errorf("workspaces = %v, want w9 closed and only w1 left beside the worktree one", server.Workspaces)
	}
}
