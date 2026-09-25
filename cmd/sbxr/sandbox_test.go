package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
	"github.com/swat9013/sbxr/internal/sandbox"
)

const lifecycleUserConfig = "version: 1\ngit:\n  name: tester\n  email: tester@example.com\n"

// lifecycle は sbx stub と一時ディレクトリの上で sbxr のライフサイクルを動かす。
type lifecycle struct {
	deps     dependencies
	stub     *sbxstub.Stub
	places   sandbox.Places
	prompter *fakePrompter
	// clonedRepoDecl は fake clone が作る repo に置く repo 宣言。空なら置かない。
	clonedRepoDecl string
	clones         []string
}

func newLifecycle(t *testing.T, userConfig string) *lifecycle {
	t.Helper()
	root := t.TempDir()
	lc := &lifecycle{
		stub:     &sbxstub.Stub{},
		prompter: &fakePrompter{},
		places: sandbox.Places{
			StateRoot:  filepath.Join(root, "state", "sbxr", "sandboxes"),
			CacheRoot:  filepath.Join(root, "cache", "sbxr", "repos"),
			UserConfig: filepath.Join(root, "config.yaml"),
		},
	}
	if err := os.WriteFile(lc.places.UserConfig, []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	lc.deps = dependencies{
		runtime:        runtime.NewSbx(lc.stub.Run),
		userConfigPath: fixedPath(lc.places.UserConfig),
		secretFilePath: fixedPath(filepath.Join(root, "secrets.env")),
		prompter:       lc.prompter,
		places:         func() (sandbox.Places, error) { return lc.places, nil },
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

// localRepo は repo 宣言を持つローカル repo を作る。
func localRepo(t *testing.T, name, repoDecl string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if repoDecl != "" {
		if err := os.WriteFile(filepath.Join(dir, "sbxr.yaml"), []byte(repoDecl), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
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

// --- create → destroy の往復 ---

func TestCreateThenDestroyOfAGitURLLeavesNoStateDirOrCacheClone(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	url := "https://example.com/me/app.git"
	lc.mustRun(t, "create", url, "--yes")
	if !exists(lc.places.StateDir("app")) || !exists(filepath.Join(lc.places.CacheRoot, "app")) {
		t.Fatalf("create did not leave a state dir and a cache clone")
	}
	lc.mustRun(t, "stop", url)

	lc.mustRun(t, "destroy", url, "--yes")

	if exists(lc.places.StateDir("app")) || exists(filepath.Join(lc.places.CacheRoot, "app")) {
		t.Errorf("destroy left the state dir or the cache clone")
	}
	if len(lc.stub.Sandboxes) != 0 {
		t.Errorf("sandboxes = %v, want none", lc.stub.Sandboxes)
	}
}

func TestDestroyRemovesSecretsPlacedBeforeAFailedSandboxCreation(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+"secrets: [github]\n")
	secretFile, _ := lc.deps.secretFilePath()
	if err := os.WriteFile(secretFile, []byte("GITHUB_TOKEN=ghp_x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := localRepo(t, "app", "")
	lc.stub.FailOn = "env create"
	if _, err := lc.run(t, "create", repo, "--yes"); err == nil {
		t.Fatalf("create error = nil, want the injected env create failure")
	}
	lc.stub.FailOn = ""

	lc.mustRun(t, "destroy", repo, "--yes")

	if lc.stub.SandboxSecrets["app"] != 0 {
		t.Errorf("sandbox-scoped secrets = %d, want the token removed", lc.stub.SandboxSecrets["app"])
	}
	if exists(lc.places.StateDir("app")) {
		t.Errorf("destroy left the state dir")
	}
}

func TestDestroyOfAPathRepoKeepsTheRepo(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	lc.mustRun(t, "stop", repo)

	lc.mustRun(t, "destroy", repo, "--yes")

	if !exists(repo) {
		t.Errorf("destroy removed the user's repo %s", repo)
	}
	if exists(lc.places.StateDir("app")) {
		t.Errorf("destroy left the state dir")
	}
}

// --- create ---

func TestCreateWritesTheEnvDefinitionAndDeclarationToTheStateDir(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", repoWithEgress)

	lc.mustRun(t, "create", repo, "--yes")

	env, err := os.ReadFile(filepath.Join(lc.places.StateDir("app"), "sbxenv.yaml"))
	if err != nil || !strings.Contains(string(env), "path: "+repo) || !strings.Contains(string(env), "name: app") {
		t.Errorf("sbxenv.yaml = %q, %v, want the repo as the workspace", env, err)
	}
	decl, err := os.ReadFile(filepath.Join(lc.places.StateDir("app"), "declaration.yaml"))
	if err != nil || !strings.Contains(string(decl), "api.example.com:443") || !strings.Contains(string(decl), "tester@example.com") {
		t.Errorf("declaration.yaml = %q, %v, want the resolved declaration", decl, err)
	}
}

func TestCreateAddsRepoEgressAsSandboxScopeRulesAfterCreatingTheSandbox(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", repoWithEgress)

	lc.mustRun(t, "create", repo, "--yes")

	if got := lc.stub.SandboxRules["app"]; !slices.Equal(got, []string{"api.example.com:443"}) {
		t.Errorf("sandbox rules = %v, want the repo egress", got)
	}
}

func TestCreateFromAGitURLWithYesDropsTheRepoEgress(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.clonedRepoDecl = repoWithEgress

	out := lc.mustRun(t, "create", "https://example.com/me/app.git", "--yes")

	if len(lc.stub.SandboxRules["app"]) != 0 {
		t.Errorf("sandbox rules = %v, want none from an unreviewed git URL", lc.stub.SandboxRules["app"])
	}
	if !strings.Contains(out, "egress (1 件) を落とした") {
		t.Errorf("output = %q, want it to say the repo egress was dropped", out)
	}
}

func TestCreateFromAGitURLKeepsTheRepoEgressWhenAHumanApproves(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.clonedRepoDecl = repoWithEgress
	lc.prompter.confirms = []bool{true}

	lc.mustRun(t, "create", "https://example.com/me/app.git")

	if got := lc.stub.SandboxRules["app"]; !slices.Equal(got, []string{"api.example.com:443"}) {
		t.Errorf("sandbox rules = %v, want the approved repo egress", got)
	}
}

func TestCreateWiresRequestedSecretsBeforeCreatingTheSandbox(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+"secrets: [github]\n")
	secretFile, _ := lc.deps.secretFilePath()
	if err := os.WriteFile(secretFile, []byte("GITHUB_TOKEN=ghp_x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := localRepo(t, "app", "")

	lc.mustRun(t, "create", repo, "--yes")

	secretAt := slices.Index(lc.stub.Writes, "secret set github --sandbox app")
	createAt := slices.IndexFunc(lc.stub.Writes, func(w string) bool { return strings.HasPrefix(w, "env create") })
	if secretAt < 0 || createAt < 0 || secretAt > createAt {
		t.Errorf("sbx writes = %q, want the github secret set before env create", lc.stub.Writes)
	}
}

func TestCreateStopsOnASecretRequestWithoutADefinition(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "version: 1\nsecrets: [jira]\n")

	_, err := lc.run(t, "create", repo, "--yes")

	if err == nil || !strings.Contains(err.Error(), "jira") {
		t.Errorf("error = %v, want it to name the undefined secret", err)
	}
	if len(lc.stub.Writes) != 0 || exists(lc.places.StateDir("app")) {
		t.Errorf("create wrote %q / a state dir despite the missing definition", lc.stub.Writes)
	}
}

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

func TestCreateDeclinedAtTheGateCreatesNothing(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.prompter.confirms = []bool{false}
	repo := localRepo(t, "app", "")

	_, err := lc.run(t, "create", repo)

	if err == nil {
		t.Errorf("create error = nil, want declining to exit non-zero")
	}
	if len(lc.stub.Writes) != 0 {
		t.Errorf("sbx writes = %q, want none after declining", lc.stub.Writes)
	}
	if exists(lc.places.StateDir("app")) {
		t.Errorf("state dir was written after declining")
	}
}

func TestCreateOfAGitURLDeclinedAtTheGateLeavesNoCacheClone(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.prompter.confirms = []bool{false}

	_, err := lc.run(t, "create", "https://example.com/me/app.git")

	if err == nil {
		t.Errorf("create error = nil, want declining to exit non-zero")
	}
	if exists(filepath.Join(lc.places.CacheRoot, "app")) {
		t.Errorf("cache clone was left behind; destroy cannot find it without a state dir")
	}
}

func TestCreateOfAGitURLClonesAgainInsteadOfReusingALeftoverClone(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	leftover := filepath.Join(lc.places.CacheRoot, "app")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "sbxr.yaml"), []byte(repoWithEgress), 0o600); err != nil {
		t.Fatal(err)
	}

	lc.prompter.confirms = []bool{true}

	lc.mustRun(t, "create", "https://example.com/bob/app.git")

	if len(lc.clones) != 1 {
		t.Errorf("clones = %v, want a fresh clone", lc.clones)
	}
	if len(lc.stub.SandboxRules["app"]) != 0 {
		t.Errorf("sandbox rules = %v, want none from the leftover clone's declaration", lc.stub.SandboxRules["app"])
	}
}

func TestPlanOfAGitURLLeavesNoCacheClone(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.clonedRepoDecl = repoWithEgress

	out := lc.mustRun(t, "plan", "https://example.com/me/app.git")

	if !strings.Contains(out, "api.example.com:443") {
		t.Errorf("output = %q, want the cloned declaration", out)
	}
	if exists(filepath.Join(lc.places.CacheRoot, "app")) {
		t.Errorf("plan left a cache clone")
	}
}

func TestCreateRefusesARepoWhoseNameIsTakenByAnotherRepo(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.mustRun(t, "create", localRepo(t, "app", ""), "--yes")
	other := localRepo(t, "app", "")

	_, err := lc.run(t, "create", other, "--yes")

	if err == nil || !strings.Contains(err.Error(), "別の repo") {
		t.Errorf("error = %v, want it to refuse a name taken by another repo", err)
	}
}

func TestDestroyRefusesARepoWhoseNameIsTakenByAnotherRepo(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.mustRun(t, "create", localRepo(t, "app", ""), "--yes")
	other := localRepo(t, "app", "")

	_, err := lc.run(t, "destroy", other, "--yes", "--force")

	if err == nil || !strings.Contains(err.Error(), "別の repo") {
		t.Errorf("error = %v, want it to refuse a name taken by another repo", err)
	}
	if _, ok := lc.stub.Sandboxes["app"]; !ok {
		t.Errorf("the other repo's sandbox was removed")
	}
}

func TestCreateAfterACreationThatStoppedHalfwayAsksToDestroyFirst(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", repoWithEgress)
	lc.stub.FailOn = "policy allow network --sandbox"
	if _, err := lc.run(t, "create", repo, "--yes"); err == nil {
		t.Fatalf("create error = nil, want the injected rule failure")
	}
	lc.stub.FailOn = ""

	_, err := lc.run(t, "create", repo, "--yes")

	if err == nil || !strings.Contains(err.Error(), "途中で止まっている") {
		t.Errorf("error = %v, want the half-created sandbox reported instead of success", err)
	}
}

func TestDestroyWorksAfterTheLocalRepoWasDeleted(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	lc.mustRun(t, "stop", repo)
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	lc.mustRun(t, "destroy", repo, "--yes")

	if _, ok := lc.stub.Sandboxes["app"]; ok {
		t.Errorf("sandbox is still there")
	}
}

func TestCreateOnAnExistingSandboxDoesNotCreateAgain(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	writes := len(lc.stub.Writes)

	out := lc.mustRun(t, "create", repo, "--yes")

	if len(lc.stub.Writes) != writes {
		t.Errorf("sbx writes = %q, want no new writes for an existing sandbox", lc.stub.Writes[writes:])
	}
	if !strings.Contains(out, "既にある") {
		t.Errorf("output = %q, want it to say the sandbox exists", out)
	}
}

func TestCreateRefusesASandboxSbxrDidNotCreate(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.stub.Sandboxes = map[string]string{"app": "stopped"}
	repo := localRepo(t, "app", "")

	_, err := lc.run(t, "create", repo, "--yes")

	if err == nil || !strings.Contains(err.Error(), "管理外") {
		t.Errorf("error = %v, want it to refuse an unmanaged sandbox", err)
	}
}

func TestCreateFailureKeepsTheStateDirAndShowsHowToRecover(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.stub.FailOn = "env create"
	repo := localRepo(t, "app", "")

	_, err := lc.run(t, "create", repo, "--yes")

	if err == nil || !strings.Contains(err.Error(), "sbxr destroy") {
		t.Errorf("error = %v, want the recovery steps", err)
	}
	if !exists(lc.places.StateDir("app")) {
		t.Errorf("state dir was removed; destroy needs it to clean up")
	}
}

// --- destroy ---

func TestDestroyRefusesARunningSandboxWithoutForce(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")

	_, err := lc.run(t, "destroy", repo, "--yes")

	if err == nil || !strings.Contains(err.Error(), "sbxr stop") {
		t.Errorf("error = %v, want it to ask to stop first", err)
	}
	if _, ok := lc.stub.Sandboxes["app"]; !ok {
		t.Errorf("running sandbox was removed without --force")
	}
}

func TestDestroyWithForceRemovesARunningSandbox(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")

	lc.mustRun(t, "destroy", repo, "--yes", "--force")

	if _, ok := lc.stub.Sandboxes["app"]; ok {
		t.Errorf("sandbox is still there after destroy --force")
	}
}

func TestDestroyWarnsThatVMChangesAreLostAndAsks(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	lc.mustRun(t, "stop", repo)
	lc.prompter.confirms = []bool{false}

	out, err := lc.run(t, "destroy", repo)

	if !strings.Contains(out, "VM 内の commit と変更は失われる") {
		t.Errorf("output = %q, want the data-loss warning", out)
	}
	if err == nil {
		t.Errorf("destroy error = nil, want declining to exit non-zero")
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

	if err == nil || lc.stub.Sandboxes["app"] != "stopped" {
		t.Errorf("error = %v, want destroy to stop without a terminal", err)
	}
}

func TestDestroyKeepsTheStateDirWhenTheSandboxCannotBeRemoved(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	lc.mustRun(t, "stop", repo)
	lc.stub.FailOn = "env rm"

	_, err := lc.run(t, "destroy", repo, "--yes")

	if err == nil || !exists(lc.places.StateDir("app")) {
		t.Errorf("error = %v, want a failure that keeps the state dir for a retry", err)
	}
}

func TestDestroyContinuesPastACleanupFailureAndExitsNonZero(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	lc.mustRun(t, "stop", repo)
	if err := os.Chmod(lc.places.StateRoot, 0o500); err != nil { // 状態ディレクトリを消せなくする
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lc.places.StateRoot, 0o700) })

	out, err := lc.run(t, "destroy", repo, "--yes")

	if err == nil {
		t.Errorf("destroy error = nil, want non-zero after a cleanup failure")
	}
	if !strings.Contains(out, "警告") {
		t.Errorf("output = %q, want a warning", out)
	}
	if _, ok := lc.stub.Sandboxes["app"]; ok {
		t.Errorf("sandbox is still there; removal should continue past the warning")
	}
}

func TestDestroyRefusesASandboxSbxrDidNotCreate(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.stub.Sandboxes = map[string]string{"app": "stopped"}
	repo := localRepo(t, "app", "")

	_, err := lc.run(t, "destroy", repo, "--yes")

	if err == nil || !strings.Contains(err.Error(), "管理外") {
		t.Errorf("error = %v, want it to refuse an unmanaged sandbox", err)
	}
	if lc.stub.Sandboxes["app"] != "stopped" {
		t.Errorf("sandboxes = %v, want the unmanaged sandbox left alone", lc.stub.Sandboxes)
	}
}

func TestDestroyOfAGitURLDoesNotClone(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	url := "https://example.com/me/app.git"
	lc.mustRun(t, "create", url, "--yes")
	lc.clones = nil

	lc.mustRun(t, "destroy", url, "--yes", "--force")

	if len(lc.clones) != 0 {
		t.Errorf("clones = %v, want destroy to stay off the network", lc.clones)
	}
}

// --- stop / plan ---

func TestStopStopsTheSandbox(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")

	lc.mustRun(t, "stop", repo)

	if lc.stub.Sandboxes["app"] != "stopped" {
		t.Errorf("status = %q, want stopped", lc.stub.Sandboxes["app"])
	}
}

func TestPlanShowsTheMergedDeclarationWithoutChangingAnything(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", repoWithEgress)

	out := lc.mustRun(t, "plan", repo)

	for _, want := range []string{"sandbox: app", "api.example.com:443", "tester@example.com", "github.com:443"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to contain %q", out, want)
		}
	}
	if len(lc.stub.Writes) != 0 || exists(lc.places.StateDir("app")) {
		t.Errorf("plan wrote %q / a state dir", lc.stub.Writes)
	}
}

func TestARepoNameThatWouldEscapeTheStateDirIsRejected(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)

	_, err := lc.run(t, "create", "https://example.com/me/..", "--yes")

	if err == nil || !strings.Contains(err.Error(), "名前を決められない") {
		t.Errorf("error = %v, want the invalid name rejected", err)
	}
	if exists(lc.places.StateRoot) || exists(lc.places.CacheRoot) {
		t.Errorf("something was written under the state or cache root")
	}
}

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
