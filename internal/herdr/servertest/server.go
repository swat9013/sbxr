// Package servertest は test 用の VM 内の herdr server を置く。herdr の module・lifecycle の test・sbx の stub で共有する
// (sbx の stub から使うので、sbxr の package に依存しない)。
package servertest

import (
	"encoding/json"
	"fmt"
	"slices"
)

// StartupCwd は herdr machine add が起動した VM 内の server の cwd。server は起動時の workspace をここに作る
// (herdr 0.9.1 / VM の herdr v0.9.0 の実測)。
const StartupCwd = "/home/agent/workspace"

// Workspace は VM 内の herdr server の workspace と、その最初の pane の cwd。
type Workspace struct {
	ID, Cwd string
}

// Server は VM 内の herdr server を、sbxr が VM で実行する herdr workspace のコマンドの範囲で再現する。
// 応答の JSON は herdr v0.9.0 の実測の形。
type Server struct {
	Workspaces []Workspace
	// IgnoreCwd が true なら workspace create の --cwd を受け付けたふりをして StartupCwd で始める。
	IgnoreCwd bool
	// FailCreate は workspace create を失敗させる。
	FailCreate bool
	// FailClose はこの id の workspace close を失敗させる。
	FailClose []string
	created   int
}

// NewServer は herdr machine add が起動した直後の server: 起動時の workspace w1 が StartupCwd にある。
func NewServer() *Server {
	return &Server{Workspaces: []Workspace{{ID: "w1", Cwd: StartupCwd}}, created: 1}
}

// Answer は VM で実行された args が herdr workspace のコマンドなら応える。handled が false なら herdr のコマンドではない。
func (s *Server) Answer(args []string) (out []byte, handled bool, err error) {
	switch {
	case slices.Equal(args, []string{"herdr", "workspace", "list"}):
		type listed struct {
			ID string `json:"workspace_id"`
		}
		workspaces := []listed{}
		for _, w := range s.Workspaces {
			workspaces = append(workspaces, listed{ID: w.ID})
		}
		out, err = json.Marshal(map[string]any{"id": "cli:workspace:list", "result": map[string]any{"type": "workspace_list", "workspaces": workspaces}})
		return out, true, err
	case len(args) == 6 && slices.Equal(args[:4], []string{"herdr", "workspace", "create", "--cwd"}) && args[5] == "--focus":
		if s.FailCreate {
			return nil, true, fmt.Errorf("exit status 1")
		}
		cwd := args[4]
		if s.IgnoreCwd {
			cwd = StartupCwd
		}
		s.created++
		w := Workspace{ID: fmt.Sprintf("w%d", s.created), Cwd: cwd}
		s.Workspaces = append(s.Workspaces, w)
		out, err = json.Marshal(map[string]any{"id": "cli:workspace:create", "result": map[string]any{
			"type":      "workspace_created",
			"workspace": map[string]any{"workspace_id": w.ID},
			"root_pane": map[string]any{"cwd": w.Cwd},
		}})
		return out, true, err
	case len(args) == 4 && slices.Equal(args[:3], []string{"herdr", "workspace", "close"}):
		if slices.Contains(s.FailClose, args[3]) {
			return nil, true, fmt.Errorf("exit status 1")
		}
		i := slices.IndexFunc(s.Workspaces, func(w Workspace) bool { return w.ID == args[3] })
		if i < 0 {
			return nil, true, fmt.Errorf("workspace %s が無い", args[3])
		}
		s.Workspaces = slices.Delete(s.Workspaces, i, i+1)
		out, err = json.Marshal(map[string]any{"id": "cli:workspace:close", "result": map[string]any{"type": "ok"}})
		return out, true, err
	}
	return nil, false, nil
}
