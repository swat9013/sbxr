package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/herdr/herdrtest"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
	"github.com/swat9013/sbxr/internal/sandbox"
)

// plan・create・stop・destroy の振る舞い (状態機械の遷移と、それに伴う片付け・前提の確認) は internal/sandbox の遷移表が固定する。
// cmd に置くのは、引数と flag の解釈と表示 (この file) と、sbx stub の上の受け入れテスト (ADR 0001):
// 宣言の merge と egress・boot・確認関門 (acceptance_test.go)、VM の中の段と egress 自己検証が sbx stub の VM で通ること
// (materialize_test.go・egresscheck_test.go)、v0.1.0 の状態ディレクトリを扱えること (compat_test.go)。

const lifecycleUserConfig = "version: 1\ngit:\n  name: tester\n  email: tester@example.com\n"

// lifecycle は sbx stub と一時ディレクトリの上で sbxr のライフサイクルを動かす。
type lifecycle struct {
	deps       dependencies
	stub       *sbxstub.Stub
	herdr      *herdrtest.Fake
	places     sandbox.Places
	userConfig string
	prompter   *fakePrompter
	// clonedRepoDecl は fake clone が作る repo に置く repo 宣言。空なら置かない。
	clonedRepoDecl string
	clones         []string
}

func newLifecycle(t *testing.T, userConfig string) *lifecycle {
	t.Helper()
	root := t.TempDir()
	lc := &lifecycle{
		stub:     &sbxstub.Stub{VM: &sbxstub.FakeVM{}},
		herdr:    &herdrtest.Fake{},
		prompter: &fakePrompter{},
		places: sandbox.Places{
			StateRoot: filepath.Join(root, "state", "sbxr", "sandboxes"),
			CacheRoot: filepath.Join(root, "cache", "sbxr", "repos"),
		},
		userConfig: filepath.Join(root, "config.yaml"),
	}
	if err := os.WriteFile(lc.userConfig, []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	lc.deps = dependencies{
		runtime:        runtime.NewSbx(lc.stub.Run),
		userConfigPath: fixedPath(lc.userConfig),
		secretFilePath: fixedPath(filepath.Join(root, "secrets.env")),
		prompter:       lc.prompter,
		places:         func() (sandbox.Places, error) { return lc.places, nil },
		herdr:          lc.herdr,
		clone: func(_ context.Context, url, dir string) error {
			lc.clones = append(lc.clones, url)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			if lc.clonedRepoDecl == "" {
				return nil
			}
			return os.WriteFile(filepath.Join(dir, "sbxr.yaml"), []byte(lc.clonedRepoDecl), 0o600)
		},
	}
	return lc
}

// herdrLifecycle は herdr 連携を有効にした user 設定と、kit startup を終えた VM で動かす。
func herdrLifecycle(t *testing.T) *lifecycle {
	t.Helper()
	lc := newLifecycle(t, lifecycleUserConfig+"herdr:\n  enabled: true\n")
	lc.stub.VM.CompleteKitStartup()
	return lc
}

// localRepo は repo 宣言を持つローカル repo を作る。
func localRepo(t *testing.T, name, repoDecl string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if repoDecl != "" {
		writeRepoDecl(t, dir, repoDecl)
	}
	return dir
}

func writeRepoDecl(t *testing.T, repo, decl string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "sbxr.yaml"), []byte(decl), 0o600); err != nil {
		t.Fatal(err)
	}
}

// stateDir は sandbox VM の状態ディレクトリ (状態の置き場の下の <名前>。ADR 0006)。中のファイルは読まない。
func (lc *lifecycle) stateDir(name string) string {
	return filepath.Join(lc.places.StateRoot, name)
}

func (lc *lifecycle) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runSbxr(t, lc.deps, args...)
}

func (lc *lifecycle) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := lc.run(t, args...)
	if err != nil {
		t.Fatalf("sbxr %s error = %v, output = %q", strings.Join(args, " "), err, out)
	}
	return out
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

const repoWithEgress = "version: 1\negress:\n  api:\n    rationale: test\n    allow: [api.example.com:443]\n"

// --- 表示 ---

func TestEachCommandNamesTheSandboxOfAGitURLInItsResult(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	url := "https://example.com/me/app.git"

	for _, step := range []struct {
		args []string
		want string
	}{
		{[]string{"create", url, "--yes"}, "sandbox VM app を作った"},
		{[]string{"create", url, "--yes"}, "sandbox VM app は既にある"},
		{[]string{"stop", url}, "sandbox VM app を止めた"},
		{[]string{"stop", url}, "sandbox VM app は止まっている"},
		{[]string{"destroy", url, "--yes"}, "sandbox VM app を撤去した"},
	} {
		// 各段は前の段が残した状態から始まる (作った → 既にある → 止めた → 止まっている → 撤去した)
		t.Run(step.want, func(t *testing.T) {
			if out := lc.mustRun(t, step.args...); !strings.Contains(out, step.want) {
				t.Errorf("sbxr %s output = %q, want %q", strings.Join(step.args, " "), out, step.want)
			}
		})
	}
}

func TestPlanShowsTheSummaryAndTheDriftOfAnExistingSandbox(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	writeRepoDecl(t, repo, "version: 1\ninit: [make setup]\n")

	out := lc.mustRun(t, "plan", repo)

	if !strings.Contains(out, "sandbox: app") || !strings.Contains(out, "差分が 1 箇所") || !strings.Contains(out, "  init\n    作成時: []\n    現在:   [\"make setup\"]") {
		t.Errorf("output = %q, want the summary and the init difference", out)
	}
}

func TestCreateOfAChangedSandboxShowsTheDriftAndExitsNonZero(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	writeRepoDecl(t, repo, repoWithEgress)

	out, err := lc.run(t, "create", repo)

	if err == nil || !strings.Contains(out, "sandbox_egress") || !strings.Contains(err.Error(), "sbxr destroy "+repo) {
		t.Errorf("error = %v, output = %q, want the drift shown with how to recreate", err, out)
	}
	if len(lc.prompter.prompts) != 0 {
		t.Errorf("prompts = %v, want no gate for an existing sandbox", lc.prompter.prompts)
	}
}

// --- 確認関門 (--yes と端末) ---

func TestCreateWithoutATerminalOrYesStopsBeforeCreating(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.prompter.noTerminal = true
	repo := localRepo(t, "app", "")

	_, err := lc.run(t, "create", repo)

	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error = %v, want it to point at --yes", err)
	}
	if len(lc.stub.Writes) != 0 {
		t.Errorf("sbx writes = %q, want none without confirmation", lc.stub.Writes)
	}
}

func TestDestroyAsksAfterShowingThatVMChangesAreLost(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	lc.mustRun(t, "stop", repo)
	lc.prompter.confirms = []bool{false}

	out, err := lc.run(t, "destroy", repo)

	if err == nil || !strings.Contains(out, "VM 内の commit と変更は失われる") || len(lc.prompter.prompts) != 1 {
		t.Errorf("error = %v, output = %q, prompts = %v, want the data loss shown and one question", err, out, lc.prompter.prompts)
	}
	if lc.stub.Sandboxes["app"] != "stopped" {
		t.Errorf("sandboxes = %v, want nothing removed after declining", lc.stub.Sandboxes)
	}
}

func TestDestroyWithoutATerminalOrYesStops(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	lc.mustRun(t, "stop", repo)
	lc.prompter.noTerminal = true

	_, err := lc.run(t, "destroy", repo)

	if err == nil || !strings.Contains(err.Error(), "--yes") || lc.stub.Sandboxes["app"] != "stopped" {
		t.Errorf("error = %v, want destroy to stop without a terminal and point at --yes", err)
	}
}

func TestDestroyWithForceRemovesARunningSandbox(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")

	if _, err := lc.run(t, "destroy", repo, "--yes"); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("destroy without --force error = %v, want it refused with --force suggested", err)
	}
	lc.mustRun(t, "destroy", repo, "--yes", "--force")

	if _, ok := lc.stub.Sandboxes["app"]; ok {
		t.Errorf("sandbox is still there after destroy --force")
	}
}

// --- 置き場 ---

func TestDefaultPlacesFollowAnAbsoluteXDGDirectory(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg/state")

	places, err := defaultPlaces()

	if err != nil || places.StateRoot != "/xdg/state/sbxr/sandboxes" {
		t.Errorf("StateRoot = %q, %v, want it under XDG_STATE_HOME", places.StateRoot, err)
	}
}

func TestDefaultPlacesIgnoreARelativeXDGDirectory(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "relative/is/ignored")

	places, err := defaultPlaces()

	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if places.CacheRoot != filepath.Join(home, ".cache", "sbxr", "repos") {
		t.Errorf("CacheRoot = %q, want the ~/.cache fallback for a relative XDG_CACHE_HOME", places.CacheRoot)
	}
}
