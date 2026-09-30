package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/herdr/herdrtest"
	"github.com/swat9013/sbxr/internal/herdr/servertest"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/inmemory"
	"github.com/swat9013/sbxr/internal/secret"
)

const testUserConfig = "version: 1\ngit:\n  name: tester\n  email: tester@example.com\n"

const herdrUserConfig = testUserConfig + "herdr:\n  enabled: true\n  version: v0.9.0\n"

const repoWithEgress = "version: 1\negress:\n  api:\n    rationale: test\n    allow: [api.example.com:443]\n"

const appURL = "https://example.com/me/app.git"

// world は in-memory の実行基盤・fake の host の herdr・一時ディレクトリの置き場の上で、lifecycle を動かす。
type world struct {
	t     *testing.T
	vms   *inmemory.Runtime
	herdr *herdrtest.Fake
	// herdrServer は VM 内の herdr server (herdr machine add が起動した直後の状態から始まる)。
	herdrServer *servertest.Server
	// places と userConfig は一時ディレクトリの下。
	places     Places
	userConfig string
	secrets    secret.Values
	// clonedRepoDecl は fake の clone が作る repo に置く repo 宣言。空なら置かない。
	clonedRepoDecl string
	clones         []string
	// repoPath は表の sandbox VM app のローカル repo (repo() が作る)。
	repoPath  string
	out, errs strings.Builder

	// 実行基盤に起こす失敗
	failDefine, failEnvCreate, failRemove bool
	// failCreationRecord なら、定義を書いた後に状態ディレクトリを書き込めなくする (作成の最初の記録を書けない)。
	failCreationRecord bool
	failAfterCreated   runtime.CreatedStep
	// stops は StopSandbox を呼ばれた VM (arrive が着いた後から数える)。
	stops []string
	// definitionsAtArrival は arrive が着いたときの実行基盤の定義の数。
	definitionsAtArrival int
}

func newWorld(t *testing.T, userConfig string) *world {
	t.Helper()
	root := t.TempDir()
	w := &world{
		t:           t,
		vms:         inmemory.New(),
		herdr:       &herdrtest.Fake{},
		herdrServer: servertest.NewServer(),
		places:      Places{StateRoot: filepath.Join(root, "state"), CacheRoot: filepath.Join(root, "cache")},
		userConfig:  filepath.Join(root, "config.yaml"),
		secrets:     secret.Values{},
	}
	// 作成の段 (materialize・read-back・egress 自己検証・herdr の workspace) が VM に送るコマンドに答える
	answers := vmAnswers(nil)
	w.vms.Respond = func(sandbox string, command runtime.SandboxCommand) ([]byte, error) {
		if out, handled, err := w.herdrServer.Answer(command.Args); handled {
			return out, err
		}
		return answers(sandbox, command)
	}
	if err := os.WriteFile(w.userConfig, []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *world) lifecycle() Lifecycle {
	return Lifecycle{
		Runtime:     worldRuntime{Runtime: w.vms, w: w},
		Herdr:       w.herdr,
		Places:      w.places,
		UserConfig:  w.userConfig,
		Clone:       w.clone,
		ReadSecrets: func() (secret.Values, error) { return w.secrets, nil },
		Output:      &w.out,
		Errors:      &w.errs,
	}
}

func (w *world) clone(_ context.Context, url, dir string) error {
	w.clones = append(w.clones, url)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if w.clonedRepoDecl == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(dir, repoDeclarationFile), []byte(w.clonedRepoDecl), 0o600)
}

// localRepo は repo 宣言を持つローカル repo を作る。
func localRepo(t *testing.T, name, repoDecl string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if repoDecl != "" {
		if err := os.WriteFile(filepath.Join(dir, repoDeclarationFile), []byte(repoDecl), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// --- 入口の呼び出し ---

func (w *world) create(repo string, gate Gate) (CreateResult, error) {
	return w.lifecycle().Create(context.Background(), repo, gate)
}

func (w *world) mustCreate(repo string) {
	w.t.Helper()
	if _, err := w.create(repo, unattended()); err != nil {
		w.t.Fatalf("Create(%s) error = %v", repo, err)
	}
}

func (w *world) stop(repo string) (StopOutcome, error) {
	result, err := w.lifecycle().Stop(context.Background(), repo)
	return result.Outcome, err
}

func (w *world) mustStop(repo string) {
	w.t.Helper()
	if _, err := w.stop(repo); err != nil {
		w.t.Fatalf("Stop(%s) error = %v", repo, err)
	}
}

func (w *world) destroy(repo string, running RunningPolicy) error {
	_, err := w.lifecycle().Destroy(context.Background(), repo, unattended(), running)
	return err
}

func (w *world) plan(repo string) (PlanResult, error) {
	return w.lifecycle().Plan(context.Background(), repo)
}

// stateOf は repo の sandbox VM が状態機械のどこにいるかを返す。
func (w *world) stateOf(repo string) state {
	w.t.Helper()
	target, err := resolveTarget(repo, w.places.CacheRoot)
	if err != nil {
		w.t.Fatal(err)
	}
	observed, err := w.lifecycle().inspect(context.Background(), target)
	if err != nil {
		w.t.Fatal(err)
	}
	return observed.state
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// --- 確認関門 ---

// fakeGate は確認関門。見せられたものを覚え、決めた答えを返す。
type fakeGate struct {
	unattended bool
	answer     bool
	err        error
	proposals  []Proposal
}

func (g *fakeGate) Approve(_ context.Context, proposal Proposal) (bool, error) {
	g.proposals = append(g.proposals, proposal)
	return g.answer, g.err
}

func (g *fakeGate) Unattended() bool { return g.unattended }

// unattended は --yes の確認関門 (人間が見ていない)。
func unattended() *fakeGate { return &fakeGate{unattended: true, answer: true} }

// approvedByHuman は人間が承認する確認関門。
func approvedByHuman() *fakeGate { return &fakeGate{answer: true} }

// declinedByHuman は人間が断る確認関門。
func declinedByHuman() *fakeGate { return &fakeGate{} }

// --- 実行基盤 ---

// worldRuntime は world が決めた失敗を起こす実行基盤。
type worldRuntime struct {
	*inmemory.Runtime
	w *world
}

func (r worldRuntime) DefineSandbox(stateDir string, spec runtime.SandboxSpec) error {
	if r.w.failDefine {
		return errors.New("定義を書けない")
	}
	if r.w.failCreationRecord {
		if err := os.Chmod(stateDir, 0o500); err != nil {
			return err
		}
		r.w.t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })
	}
	return r.Runtime.DefineSandbox(stateDir, spec)
}

// CreateSandbox は、failEnvCreate なら sandbox スコープの secret を置いた後に VM を作れずに失敗する (sbx の env create の失敗)。
// failAfterCreated なら VM を作れた後の段で失敗する。
func (r worldRuntime) CreateSandbox(ctx context.Context, stateDir string, spec runtime.SandboxSpec) error {
	if r.w.failEnvCreate {
		placed := r.Sandbox(spec.Name)
		placed.Secrets = append(placed.Secrets, spec.Secrets...)
		return errors.New("env create に失敗した")
	}
	if err := r.Runtime.CreateSandbox(ctx, stateDir, spec); err != nil {
		return err
	}
	if r.w.failAfterCreated != 0 {
		return &runtime.CreatedError{Step: r.w.failAfterCreated, Err: errors.New("VM を作った後の段が失敗した")}
	}
	return nil
}

func (r worldRuntime) StopSandbox(ctx context.Context, sandbox string) error {
	r.w.stops = append(r.w.stops, sandbox)
	return r.Runtime.StopSandbox(ctx, sandbox)
}

func (r worldRuntime) RemoveEnvironment(ctx context.Context, envDir string) error {
	if r.w.failRemove {
		return errors.New("env rm に失敗した")
	}
	return r.Runtime.RemoveEnvironment(ctx, envDir)
}

// vmAnswers は作成の段 (materialize・read-back・egress 自己検証) が VM に送るコマンドに答える。
// git config は書いた値を覚えて返し、plugin の一覧は空、probe は answerProbes と同じに答える。
func vmAnswers(allowed []string) func(string, runtime.SandboxCommand) ([]byte, error) {
	gitConfig := map[string]string{}
	probes := answerProbes(allowed)
	return func(_ string, command runtime.SandboxCommand) ([]byte, error) {
		args := command.Args
		switch {
		case slices.Equal(args, []string{"printenv", "HOME"}):
			return []byte("/home/agent\n"), nil
		case len(args) == 6 && args[0] == "git" && args[3] == "--replace-all":
			gitConfig[args[4]] = args[5]
			return nil, nil
		case len(args) == 5 && args[0] == "git" && args[3] == "--get-all":
			return []byte(gitConfig[args[4]] + "\n"), nil
		case len(args) >= 3 && args[0] == "claude" && args[1] == "plugin" && slices.Contains(args, "--json"):
			return []byte("[]"), nil
		}
		return probes(command)
	}
}
