package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/herdr"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/secret"
)

// 遷移表 (transitions_test.go) が見ない、遷移に伴う action と、状態機械の外 (管理外・別出所) の振る舞い。

const gitlabUserConfig = testUserConfig + `secrets: [gitlab]
secret_defs:
  gitlab:
    key: GITLAB_TOKEN
    hosts: [gitlab.example.com]
    env: GITLAB_TOKEN
    vars:
      GITLAB_HOST: gitlab.example.com
egress:
  gitlab:
    rationale: test
    allow: [gitlab.example.com:443]
`

// --- create: 作る内容 ---

func TestCreateDefinesTheSandboxWithItsNameRepoAndEnv(t *testing.T) {
	w := newWorld(t, gitlabUserConfig)
	w.secrets = secret.Values{"GITLAB_TOKEN": "glpat_x"}
	repo := localRepo(t, "app", "")

	w.mustCreate(repo)

	got := w.vms.Definitions[w.places.stateDirPath("app")]
	if got.Name != "app" || got.Repo != repo || !reflect.DeepEqual(got.Env, map[string]string{"GITLAB_HOST": "gitlab.example.com"}) {
		t.Errorf("definition = %+v, want app from %s with GITLAB_HOST", got, repo)
	}
}

// boot を宣言していなくても再生を頼む (再生の仕組みの無い VM を実 sbx で確かめていないので、作る VM の形を変えない)。
func TestCreateAsksTheRuntimeToReplayBootEvenWithoutADeclaredBoot(t *testing.T) {
	w := newWorld(t, testUserConfig)

	w.mustCreate(localRepo(t, "app", ""))

	if !w.vms.Definitions[w.places.stateDirPath("app")].ReplayBoot {
		t.Errorf("ReplayBoot = false, want the replay asked for")
	}
}

func TestCreatePlacesTheWiredSecretValueAndTheRepoEgressAsSandboxRules(t *testing.T) {
	w := newWorld(t, gitlabUserConfig)
	w.secrets = secret.Values{"GITLAB_TOKEN": "glpat_x"}

	w.mustCreate(localRepo(t, "app", repoWithEgress))

	sb := w.vms.Sandbox("app")
	wantSecrets := []runtime.SandboxSecret{{Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Value: "glpat_x"}}
	if !reflect.DeepEqual(sb.Secrets, wantSecrets) || !slices.Equal(sb.EgressRules, []string{"api.example.com:443"}) {
		t.Errorf("secrets = %+v, rules = %q, want the token and the repo egress", sb.Secrets, sb.EgressRules)
	}
}

func TestCreateRecordsTheDeclarationSoThatPlanFindsNoDrift(t *testing.T) {
	w := newWorld(t, testUserConfig)
	repo := localRepo(t, "app", repoWithEgress)
	w.mustCreate(repo)

	result, err := w.plan(repo)

	if err != nil || result.Drift == nil || len(result.Drift.Differences) != 0 {
		t.Errorf("Plan() = %+v, %v, want the recorded declaration with no drift", result, err)
	}
}

// --- create: 確認関門 ---

func TestCreateShowsWhatItCreatesAtTheGateBeforeCreatingAnything(t *testing.T) {
	w := newWorld(t, testUserConfig)
	gate := declinedByHuman()

	_, err := w.create(localRepo(t, "app", repoWithEgress+"init: [make setup]\n"), gate)

	if !errors.Is(err, errDeclined) {
		t.Errorf("Create() error = %v, want declining to stop the creation", err)
	}
	if len(gate.proposals) != 1 || !strings.Contains(gate.proposals[0].Summary, "api.example.com:443") || !strings.Contains(gate.proposals[0].Summary, "make setup") {
		t.Errorf("proposals = %+v, want the merged declaration shown once", gate.proposals)
	}
	if exists(w.places.stateDirPath("app")) || len(w.vms.Sandboxes) != 0 {
		t.Errorf("state dir or sandbox was created after declining")
	}
}

func TestCreateOfAGitURLDeclinedAtTheGateLeavesNoCacheClone(t *testing.T) {
	w := newWorld(t, testUserConfig)

	_, err := w.create(appURL, declinedByHuman())

	if err == nil || exists(filepath.Join(w.places.CacheRoot, "app")) {
		t.Errorf("Create() error = %v, want declining to leave no cache clone", err)
	}
}

func TestCreateStopsWhenTheGateCannotAsk(t *testing.T) {
	w := newWorld(t, testUserConfig)
	noTerminal := errors.New("確認には端末が要る")

	_, err := w.create(localRepo(t, "app", ""), &fakeGate{err: noTerminal})

	if !errors.Is(err, noTerminal) || exists(w.places.stateDirPath("app")) {
		t.Errorf("Create() error = %v, want the gate's error before anything is created", err)
	}
}

func TestCreateOfAGitURLWithoutAHumanDropsTheRepoEgressAndSaysSo(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.clonedRepoDecl = repoWithEgress
	gate := unattended()

	if _, err := w.create(appURL, gate); err != nil {
		t.Fatal(err)
	}

	if rules := w.vms.Sandbox("app").EgressRules; len(rules) != 0 {
		t.Errorf("sandbox rules = %q, want none from an unreviewed git URL", rules)
	}
	if !strings.Contains(gate.proposals[0].Summary, "egress (1 件) を落とした") {
		t.Errorf("summary = %q, want it to say the repo egress was dropped", gate.proposals[0].Summary)
	}
}

func TestCreateOfAGitURLApprovedByAHumanKeepsTheRepoEgress(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.clonedRepoDecl = repoWithEgress

	if _, err := w.create(appURL, approvedByHuman()); err != nil {
		t.Fatal(err)
	}

	if rules := w.vms.Sandbox("app").EgressRules; !slices.Equal(rules, []string{"api.example.com:443"}) {
		t.Errorf("sandbox rules = %q, want the approved repo egress", rules)
	}
}

// --- create: 前提の確認 ---

func TestCreateStopsBeforeTheStateDirWhenASecretValueIsMissing(t *testing.T) {
	w := newWorld(t, gitlabUserConfig)

	_, err := w.create(appURL, unattended())

	if err == nil || !strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Errorf("Create() error = %v, want the missing GITLAB_TOKEN", err)
	}
	if exists(w.places.stateDirPath("app")) || exists(filepath.Join(w.places.CacheRoot, "app")) || len(w.vms.Definitions) != 0 {
		t.Errorf("create left a state dir, a cache clone or a definition despite the missing value")
	}
}

func TestCreateStopsOnASecretRequestWithoutADefinition(t *testing.T) {
	w := newWorld(t, testUserConfig)

	_, err := w.create(localRepo(t, "app", "version: 1\nsecrets: [jira]\n"), unattended())

	if err == nil || !strings.Contains(err.Error(), "jira") || exists(w.places.stateDirPath("app")) {
		t.Errorf("Create() error = %v, want it to name the undefined secret before anything is written", err)
	}
}

func TestCreateStopsBeforeTheGateWhenTheHostHasNoHerdr(t *testing.T) {
	w := newWorld(t, herdrUserConfig)
	w.herdr.Missing = true
	gate := approvedByHuman()

	_, err := w.create(localRepo(t, "app", ""), gate)

	if err == nil || len(gate.proposals) != 0 || exists(w.places.stateDirPath("app")) {
		t.Errorf("Create() error = %v, proposals = %d, want the missing herdr before the gate", err, len(gate.proposals))
	}
}

// --- create: git URL の clone ---

func TestCreateOfAGitURLClonesAgainInsteadOfReusingALeftoverClone(t *testing.T) {
	w := newWorld(t, testUserConfig)
	leftover := filepath.Join(w.places.CacheRoot, "app")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, repoDeclarationFile), []byte(repoWithEgress), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := w.create("https://example.com/bob/app.git", approvedByHuman()); err != nil {
		t.Fatal(err)
	}

	if len(w.clones) != 1 || len(w.vms.Sandbox("app").EgressRules) != 0 {
		t.Errorf("clones = %v, rules = %q, want a fresh clone without the leftover's declaration", w.clones, w.vms.Sandbox("app").EgressRules)
	}
}

// --- create: 出所を記録する前の失敗 (decision/0011) ---

func TestACreationThatFailsBeforeRecordingTheSourceLeavesNoStateDirOrCacheClone(t *testing.T) {
	for name, fail := range map[string]func(w *world){
		"実行基盤の定義":  func(w *world) { w.failDefine = true },
		"作成の最初の記録": func(w *world) { w.failCreationRecord = true },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, testUserConfig)
			fail(w)

			_, err := w.create(appURL, unattended())

			if err == nil {
				t.Fatal("Create() error = nil, want the failure before the source")
			}
			if exists(w.places.stateDirPath("app")) || exists(filepath.Join(w.places.CacheRoot, "app")) {
				t.Errorf("the half-written state dir or the cache clone was left; destroy cannot find them without a source")
			}
			if strings.Contains(err.Error(), "sbxr destroy") {
				t.Errorf("error = %v, want no destroy suggested for a sandbox that is not there", err)
			}
		})
	}
}

// --- create: 復旧手順 ---

func TestCreateFailureBeforeTheVMKeepsTheStateDirAndShowsHowToRecover(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.failEnvCreate = true
	repo := localRepo(t, "app", "")

	_, err := w.create(repo, unattended())

	if err == nil || !strings.Contains(err.Error(), "sbxr destroy "+repo) || !exists(w.places.stateDirPath("app")) {
		t.Errorf("Create() error = %v, want the state dir kept and destroy suggested", err)
	}
}

func TestCreateFailureAfterTheVMAsksToStopBeforeDestroying(t *testing.T) {
	for step, stage := range map[runtime.CreatedStep]Stage{
		runtime.CreatedStepSandboxEgress: StageSandboxEgress,
		runtime.CreatedStepHerdrStartup:  StageHerdr,
	} {
		t.Run(string(stage), func(t *testing.T) {
			w := newWorld(t, testUserConfig)
			w.failAfterCreated = step
			repo := localRepo(t, "app", "")

			_, err := w.create(repo, unattended())

			var stageErr *StageError
			if !errors.As(err, &stageErr) || stageErr.Stage != stage || !strings.Contains(err.Error(), "sbxr stop "+repo+" → sbxr destroy "+repo) {
				t.Errorf("Create() error = %v, want the %s stage and stop before destroy", err, stage)
			}
		})
	}
}

func TestCreateOfAHalfCreatedSandboxAsksToCleanItUpFirst(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateIncompleteRunning)

	_, err := w.create(w.repo(), unattended())

	if err == nil || !strings.Contains(err.Error(), "途中で止まっている") || !strings.Contains(err.Error(), "sbxr stop "+w.repo()) {
		t.Errorf("Create() error = %v, want the half-created sandbox reported with stop → destroy", err)
	}
}

func TestCreateOfAVanishedVMAsksToDestroyWithoutADriftReport(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateVMGone)

	result, err := w.create(w.repo(), unattended())

	if err == nil || !strings.Contains(err.Error(), "sbxr destroy "+w.repo()) || result.Drift != nil {
		t.Errorf("Create() = %+v, %v, want destroy suggested and no drift", result, err)
	}
}

// --- create: 作成済み ---

func TestCreateOfACreatedSandboxReportsItWithoutCreatingAgain(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateRunning)
	definitions := len(w.vms.Definitions)

	result, err := w.create(w.repo(), unattended())

	if err != nil || result.Outcome != AlreadyCreated || result.Drift == nil || len(w.vms.Definitions) != definitions {
		t.Errorf("Create() = %+v, %v, want the existing sandbox reported without drift", result, err)
	}
}

func TestCreateOfACreatedSandboxWithAChangedDeclarationShowsTheDriftAndFails(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateStopped)
	if err := os.WriteFile(filepath.Join(w.repo(), repoDeclarationFile), []byte(repoWithEgress), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := w.create(w.repo(), unattended())

	if err == nil || !strings.Contains(err.Error(), "sbxr destroy "+w.repo()+" → sbxr create "+w.repo()) {
		t.Errorf("Create() error = %v, want how to recreate", err)
	}
	if result.Drift == nil || len(result.Drift.Differences) != 1 || result.Drift.Differences[0].Path != "sandbox_egress" {
		t.Errorf("drift = %+v, want the added repo egress", result.Drift)
	}
}

func TestCreateOfACreatedSandboxWhoseRepoIsGoneSaysItExists(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateRunning)
	if err := os.RemoveAll(w.repo()); err != nil {
		t.Fatal(err)
	}

	_, err := w.create(w.repo(), unattended())

	if err == nil || !strings.Contains(err.Error(), "既にある") {
		t.Errorf("Create() error = %v, want the existing sandbox named", err)
	}
}

// --- 状態機械の外: 管理外・別出所 ---

func TestEveryEntryLeavesASandboxSbxrDidNotCreateAlone(t *testing.T) {
	for name, act := range map[string]func(w *world, repo string) error{
		"create":  func(w *world, repo string) error { _, err := w.create(repo, unattended()); return err },
		"stop":    func(w *world, repo string) error { _, err := w.stop(repo); return err },
		"destroy": func(w *world, repo string) error { return w.destroy(repo, RemoveRunning) },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, testUserConfig)
			w.vms.Sandbox("app").Status = runtime.SandboxRunning

			err := act(w, localRepo(t, "app", ""))

			if err == nil || !strings.Contains(err.Error(), "管理外") || w.vms.Sandbox("app").Status != runtime.SandboxRunning {
				t.Errorf("error = %v, want the unmanaged sandbox left running", err)
			}
		})
	}
}

func TestEveryEntryLeavesASandboxOfAnotherRepoAlone(t *testing.T) {
	for name, act := range map[string]func(w *world, repo string) error{
		"create":  func(w *world, repo string) error { _, err := w.create(repo, unattended()); return err },
		"stop":    func(w *world, repo string) error { _, err := w.stop(repo); return err },
		"destroy": func(w *world, repo string) error { return w.destroy(repo, RemoveRunning) },
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, testUserConfig)
			w.arrive(stateRunning)

			err := act(w, localRepo(t, "app", ""))

			if err == nil || !strings.Contains(err.Error(), "別の repo") || w.stateOf(w.repo()) != stateRunning {
				t.Errorf("error = %v, want the other repo's sandbox left running", err)
			}
		})
	}
}

func TestARepoNameThatWouldEscapeTheStateDirIsRejected(t *testing.T) {
	w := newWorld(t, testUserConfig)

	_, err := w.create("https://example.com/me/..", unattended())

	if err == nil || !strings.Contains(err.Error(), "名前を決められない") || exists(w.places.StateRoot) || exists(w.places.CacheRoot) {
		t.Errorf("Create() error = %v, want the invalid name rejected before anything is written", err)
	}
}

// --- stop ---

func TestStopOfAStoppedVMLeavesTheVMAlone(t *testing.T) {
	for name, arrive := range map[string]func(w *world){
		"停止中":         func(w *world) { w.arrive(stateStopped) },
		"作成途中・停止":     func(w *world) { w.arrive(stateIncompleteStopped) },
		"作成途中・VM が無い": arriveHalfCreatedWithoutAVM,
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, testUserConfig)
			arrive(w)

			outcome, err := w.stop(w.repo())

			if err != nil || outcome != AlreadyStopped || len(w.stops) != 0 {
				t.Errorf("Stop() = %v, %v, sbx stops = %v, want it reported as stopped without touching the VM", outcome, err, w.stops)
			}
		})
	}
}

// arriveHalfCreatedWithoutAVM は、sbx の env create が secret を置いた後に VM を作れなかった作成途中の状態にする。
func arriveHalfCreatedWithoutAVM(w *world) {
	w.t.Helper()
	w.failEnvCreate = true
	if _, err := w.create(w.repo(), unattended()); err == nil {
		w.t.Fatal("Create() error = nil, want the creation stopped before the VM")
	}
	w.failEnvCreate = false
	if got := w.stateOf(w.repo()); got != stateIncompleteStopped || w.vms.Sandbox("app").Status != runtime.SandboxAbsent {
		w.t.Fatalf("state = %s, VM = %s, want a half-created sandbox without a VM", got, w.vms.Sandbox("app").Status)
	}
}

func TestStopOfAVanishedVMAsksToDestroyWithoutCallingTheRuntimeOrHerdr(t *testing.T) {
	w := newWorld(t, herdrUserConfig)
	w.arrive(stateVMGone)
	w.herdr.Calls = nil

	_, err := w.stop(w.repo())

	if err == nil || !strings.Contains(err.Error(), "sbxr destroy "+w.repo()) || len(w.stops) != 0 || len(w.herdr.Calls) != 0 {
		t.Errorf("Stop() error = %v, stops = %v, herdr calls = %v, want destroy suggested and nothing touched", err, w.stops, w.herdr.Calls)
	}
}

// --- destroy ---

func TestDestroyShowsThatVMChangesAreLostAtTheGate(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateStopped)
	gate := declinedByHuman()

	_, err := w.lifecycle().Destroy(t.Context(), w.repo(), gate, RefuseRunning)

	if !errors.Is(err, errDeclined) || w.stateOf(w.repo()) != stateStopped {
		t.Errorf("Destroy() error = %v, want nothing removed after declining", err)
	}
	if len(gate.proposals) != 1 || !strings.Contains(gate.proposals[0].Summary, "VM 内の commit と変更は失われる") {
		t.Errorf("proposals = %+v, want the data loss shown", gate.proposals)
	}
}

func TestDestroyRefusesAVMStartedWhileTheGateWasOpen(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateStopped)
	gate := &startingGate{fakeGate: unattended(), w: w}

	_, err := w.lifecycle().Destroy(t.Context(), w.repo(), gate, RefuseRunning)

	if err == nil || !strings.Contains(err.Error(), "sbxr stop "+w.repo()) || w.stateOf(w.repo()) != stateRunning {
		t.Errorf("Destroy() error = %v, want the VM started at the gate left alone", err)
	}
}

// startingGate は承認を求められている間に、VM が sbxr の外で起動する確認関門。
type startingGate struct {
	*fakeGate
	w *world
}

func (g *startingGate) Approve(ctx context.Context, proposal Proposal) (bool, error) {
	_ = g.w.startOutside()
	return g.fakeGate.Approve(ctx, proposal)
}

func TestDestroyOfAGitURLRemovesTheCacheCloneAndDoesNotClone(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.mustCreate(appURL)
	w.mustStop(appURL)
	w.clones = nil

	if err := w.destroy(appURL, RefuseRunning); err != nil {
		t.Fatal(err)
	}

	if exists(w.places.stateDirPath("app")) || exists(filepath.Join(w.places.CacheRoot, "app")) || len(w.clones) != 0 {
		t.Errorf("clones = %v, want the state dir and the cache clone removed without cloning", w.clones)
	}
}

func TestDestroyOfAPathRepoKeepsTheRepo(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateStopped)

	err := w.destroy(w.repo(), RefuseRunning)

	if err != nil || !exists(w.repo()) {
		t.Errorf("Destroy() = %v, want the user's repo kept", err)
	}
}

func TestDestroyWorksAfterTheLocalRepoWasDeleted(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateStopped)
	if err := os.RemoveAll(w.repo()); err != nil {
		t.Fatal(err)
	}

	err := w.destroy(w.repo(), RefuseRunning)

	if err != nil || w.stateOf(w.repo()) != stateAbsent {
		t.Errorf("Destroy() = %v, want the sandbox removed", err)
	}
}

func TestDestroyRemovesTheSandboxSecretsEvenWithoutAVM(t *testing.T) {
	for _, from := range []state{stateIncompleteStopped, stateVMGone} {
		t.Run(from.String(), func(t *testing.T) {
			w := newWorld(t, gitlabUserConfig)
			w.secrets = secret.Values{"GITLAB_TOKEN": "glpat_x"}
			repo := localRepo(t, "app", "")
			if from == stateIncompleteStopped { // sbx の env create が、secret を置いた後に VM を作れなかった
				w.failEnvCreate = true
				_, _ = w.create(repo, unattended())
				w.failEnvCreate = false
			} else {
				w.mustCreate(repo)
				_ = w.removeOutside()
			}

			if err := w.destroy(repo, RefuseRunning); err != nil {
				t.Fatal(err)
			}

			if _, ok := w.vms.Sandboxes["app"]; ok || exists(w.places.stateDirPath("app")) {
				t.Errorf("sandbox = %+v, want the secrets and the state dir removed", w.vms.Sandboxes["app"])
			}
		})
	}
}

func TestDestroyKeepsTheStateDirWhenTheSandboxCannotBeRemoved(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateStopped)
	w.failRemove = true

	err := w.destroy(w.repo(), RefuseRunning)

	if err == nil || !exists(w.places.stateDirPath("app")) {
		t.Errorf("Destroy() error = %v, want a failure that keeps the state dir for a retry", err)
	}
}

func TestDestroyContinuesPastACleanupFailureAndFailsAtTheEnd(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateStopped)
	if err := os.Chmod(w.places.StateRoot, 0o500); err != nil { // 状態ディレクトリを消せなくする
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(w.places.StateRoot, 0o700) })

	err := w.destroy(w.repo(), RefuseRunning)

	if err == nil || !strings.Contains(w.errs.String(), "警告") {
		t.Errorf("Destroy() error = %v, warnings = %q, want a warning and a failure", err, w.errs.String())
	}
	if _, ok := w.vms.Sandboxes["app"]; ok {
		t.Errorf("sandbox is still there; removal should continue past the warning")
	}
}

// --- plan ---

func TestPlanShowsTheMergedDeclarationWithoutChangingAnything(t *testing.T) {
	w := newWorld(t, testUserConfig)

	result, err := w.plan(localRepo(t, "app", repoWithEgress))

	if err != nil || !strings.Contains(result.Summary, "api.example.com:443") || !strings.Contains(result.Summary, "tester@example.com") || result.Drift != nil {
		t.Errorf("Plan() = %+v, %v, want the merged declaration without drift", result, err)
	}
	if exists(w.places.StateRoot) || len(w.vms.Sandboxes) != 0 {
		t.Errorf("plan wrote a state dir or touched the runtime")
	}
}

func TestPlanOfAGitURLReadsAFreshCloneAndLeavesNoCacheClone(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.clonedRepoDecl = repoWithEgress

	result, err := w.plan(appURL)

	if err != nil || !strings.Contains(result.Summary, "api.example.com:443") {
		t.Errorf("Plan() = %+v, %v, want the cloned declaration", result, err)
	}
	if exists(filepath.Join(w.places.CacheRoot, "app")) {
		t.Errorf("plan left a cache clone")
	}
	if strings.Contains(result.Summary, "sbxr-read-") || !strings.Contains(result.Summary, filepath.Join(w.places.CacheRoot, "app")) {
		t.Errorf("summary = %q, want the cache clone path create uses, not the temporary clone", result.Summary)
	}
}

func TestPlanOfACreatedGitURLLeavesItsCacheCloneAlone(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.mustCreate(appURL)
	marker := filepath.Join(w.places.CacheRoot, "app", "work-in-progress")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := w.plan(appURL); err != nil {
		t.Fatal(err)
	}

	if !exists(marker) {
		t.Errorf("plan replaced the sandbox's cache clone")
	}
}

func TestPlanWarnsOnceWhenItComparesWithTheRecord(t *testing.T) {
	w := newWorld(t, testUserConfig)
	repo := localRepo(t, "app", "")
	w.mustCreate(repo)
	// origin を読めない repo (.git が壊れている) は、警告して進む
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: /nowhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.errs.Reset()

	result, err := w.plan(repo)

	if err != nil || result.Drift == nil || strings.Count(w.errs.String(), "警告") != 1 {
		t.Errorf("Plan() = %v, warnings = %q, want the drift and the warning shown once", err, w.errs.String())
	}
}

func TestPlanOfAGitURLCreatedWithoutAHumanSummarizesTheRepoEgressButComparesWithoutIt(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.clonedRepoDecl = repoWithEgress
	w.mustCreate(appURL)

	result, err := w.plan(appURL)

	if err != nil || !strings.Contains(result.Summary, "api.example.com:443") {
		t.Errorf("Plan() = %+v, %v, want the repo egress in the summary", result, err)
	}
	if result.Drift == nil || len(result.Drift.Differences) != 0 {
		t.Errorf("drift = %+v, want the repo egress dropped again when comparing", result.Drift)
	}
}

// --- herdr 連携 ---

func TestHerdrIsNeverCalledWhenDisabled(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.arrive(stateStopped)

	if err := w.destroy(w.repo(), RefuseRunning); err != nil {
		t.Fatal(err)
	}

	if len(w.herdr.Calls) != 0 || w.vms.Definitions[w.places.stateDirPath("app")].Herdr != nil {
		t.Errorf("herdr calls = %v, want none and no herdr installed", w.herdr.Calls)
	}
}

func TestCreateInstallsTheDeclaredHerdrAndRegistersTheSandbox(t *testing.T) {
	w := newWorld(t, herdrUserConfig)

	w.mustCreate(localRepo(t, "app", ""))

	if got := w.vms.Definitions[w.places.stateDirPath("app")].Herdr; got == nil || got.Version != "v0.9.0" {
		t.Errorf("definition herdr = %+v, want v0.9.0", got)
	}
	if len(w.herdr.Machines) != 1 || w.herdr.Machines[0].Target != w.vms.SSHTarget("app") {
		t.Errorf("herdr machines = %v, want the VM registered once", w.herdr.Machines)
	}
}

func TestCreateKeepsTheVMAndShowsHowToRegisterWhenTheRegistrationFails(t *testing.T) {
	w := newWorld(t, herdrUserConfig)
	w.herdr.FailAdd = true
	repo := localRepo(t, "app", "")

	_, err := w.create(repo, unattended())

	if err == nil || !strings.Contains(err.Error(), "herdr machine add app.inmemory --label app") {
		t.Errorf("Create() error = %v, want how to register by hand", err)
	}
	if got := w.stateOf(repo); got != stateRunning {
		t.Errorf("state = %s, want the creation itself finished", got)
	}
}

func TestCreateLeavesALeftoverRegistrationAloneAndShowsHowToReplaceIt(t *testing.T) {
	w := newWorld(t, herdrUserConfig)
	w.herdr.Machines = []herdr.Machine{{ID: "old", Target: w.vms.SSHTarget("app")}}

	_, err := w.create(localRepo(t, "app", ""), unattended())

	if err == nil || !strings.Contains(err.Error(), "herdr machine remove old; ") || len(w.herdr.Machines) != 1 || w.herdr.Machines[0].ID != "old" {
		t.Errorf("Create() error = %v, machines = %v, want the leftover untouched and how to replace it", err, w.herdr.Machines)
	}
}

func TestStopDisablesTheHerdrMachineBeforeStoppingAndShowsHowToEnableIt(t *testing.T) {
	w := newWorld(t, herdrUserConfig)
	w.arrive(stateRunning)

	outcome, err := w.stop(w.repo())

	if err != nil || outcome != Stopped || w.herdr.Machines[0].Enabled || !strings.Contains(w.out.String(), "herdr machine enable id1") {
		t.Errorf("Stop() = %v, %v, machines = %v, output = %q, want the machine disabled and how to enable it", outcome, err, w.herdr.Machines, w.out.String())
	}
}

// 外部停止の後も、herdr が VM を起こし直さないようにする (decision/0010)。
func TestStopOfAVMStoppedOutsideSbxrDisablesItsHerdrMachineWithoutTouchingTheVM(t *testing.T) {
	w := newWorld(t, herdrUserConfig)
	w.arrive(stateRunning)
	_ = w.stopOutside()

	outcome, err := w.stop(w.repo())

	if err != nil || outcome != AlreadyStopped || w.herdr.Machines[0].Enabled || len(w.stops) != 0 {
		t.Errorf("Stop() = %v, %v, machines = %v, sbx stops = %v, want the machine disabled and the VM untouched", outcome, err, w.herdr.Machines, w.stops)
	}
	if !strings.Contains(w.out.String(), "herdr machine enable id1") {
		t.Errorf("output = %q, want how to enable the machine again", w.out.String())
	}
}

func TestStopWithoutHerdrOnTheHostLeavesTheVMRunning(t *testing.T) {
	w := newWorld(t, herdrUserConfig)
	w.arrive(stateRunning)
	w.herdr.Missing = true

	_, err := w.stop(w.repo())

	if err == nil || !strings.Contains(err.Error(), "PATH に herdr を入れる") || w.stateOf(w.repo()) != stateRunning {
		t.Errorf("Stop() error = %v, want how to put herdr on the host with the VM left running", err)
	}
}

// 作成途中の VM の machine は未登録だが、同じ名前の前の VM の解除に失敗した登録が有効なまま残りうる。
// 残っていれば無効にしてから止める (herdr が繋ぎ直して起こすと、destroy が稼働中として拒む)。
func TestStopOfAHalfCreatedVMDisablesALeftoverHerdrMachine(t *testing.T) {
	w := newWorld(t, herdrUserConfig)
	w.arrive(stateIncompleteRunning)
	w.herdr.Machines = []herdr.Machine{{ID: "old", Target: w.vms.SSHTarget("app"), Enabled: true}}

	if _, err := w.stop(w.repo()); err != nil {
		t.Fatal(err)
	}

	if w.herdr.Machines[0].Enabled || w.stateOf(w.repo()) != stateIncompleteStopped {
		t.Errorf("machines = %v, state = %s, want the leftover disabled and the VM stopped", w.herdr.Machines, w.stateOf(w.repo()))
	}
}

func TestCreateDoesNotTouchASandboxCreatedWhileTheGateWasOpen(t *testing.T) {
	w := newWorld(t, testUserConfig)
	repo := localRepo(t, "app", "")
	other := newWorld(t, testUserConfig)
	gate := &creatingGate{fakeGate: unattended(), create: func() {
		// 同じ置き場と実行基盤で、別の create が先に作り終える
		other.vms, other.places = w.vms, w.places
		other.mustCreate(repo)
	}}

	_, err := w.create(repo, gate)

	if err == nil || w.stateOf(repo) != stateRunning {
		t.Errorf("Create() error = %v, state = %s, want the other creation left running", err, w.stateOf(repo))
	}
}

// creatingGate は承認を求められている間に、同じ名前の sandbox VM が別の create で作られる確認関門。
type creatingGate struct {
	*fakeGate
	create func()
}

func (g *creatingGate) Approve(ctx context.Context, proposal Proposal) (bool, error) {
	g.create()
	return g.fakeGate.Approve(ctx, proposal)
}

func TestDestroyStopsBeforeTheGateWhenTheHostHasNoHerdr(t *testing.T) {
	w := newWorld(t, herdrUserConfig)
	w.arrive(stateStopped)
	w.herdr.Missing = true
	gate := unattended()

	_, err := w.lifecycle().Destroy(t.Context(), w.repo(), gate, RefuseRunning)

	if err == nil || len(gate.proposals) != 0 || w.stateOf(w.repo()) != stateStopped {
		t.Errorf("Destroy() error = %v, proposals = %d, want the missing herdr before the gate", err, len(gate.proposals))
	}
}

func TestDestroyRemovesTheHerdrMachineAndContinuesPastAFailure(t *testing.T) {
	for name, fail := range map[string]bool{"removed": false, "failed": true} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, herdrUserConfig)
			w.arrive(stateStopped)
			w.herdr.FailRemove = fail

			err := w.destroy(w.repo(), RefuseRunning)

			if w.stateOf(w.repo()) != stateAbsent || (err != nil) != fail {
				t.Errorf("Destroy() error = %v, want the sandbox removed and a failure only when herdr fails", err)
			}
			if fail != strings.Contains(w.errs.String(), "herdr machine remove id1") {
				t.Errorf("warnings = %q, want the removal command only when it failed", w.errs.String())
			}
		})
	}
}

// 作成途中の VM の herdr 連携は、作成の最初の記録から読む (実行基盤の定義とは食い違っても)。
func TestDestroyOfAHalfCreatedSandboxRemovesTheHerdrMachineRecordedAtTheStart(t *testing.T) {
	w := newWorld(t, herdrUserConfig)
	w.arrive(stateIncompleteRunning)
	w.herdr.Machines = []herdr.Machine{{ID: "m1", Target: w.vms.SSHTarget("app")}}
	w.vms.Definitions[w.places.stateDirPath("app")] = runtime.SandboxSpec{Name: "app"} // 定義は herdr を導入しないと答える

	if err := w.destroy(w.repo(), RemoveRunning); err != nil {
		t.Fatal(err)
	}

	if len(w.herdr.Machines) != 0 {
		t.Errorf("machines = %v, want the machine removed", w.herdr.Machines)
	}
}
