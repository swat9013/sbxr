package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/swat9013/sbxr/internal/secret"
)

// 作成済みの VM の drift (plan と、作成済みの VM への create が返す作成時の宣言との差分)。

func (w *world) writeUserConfig(config string) {
	w.t.Helper()
	if err := os.WriteFile(w.userConfig, []byte(config), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// driftOf は repo の plan が返す drift。作成済みの VM が無ければ test を止める。
func (w *world) driftOf(repo string) Comparison {
	w.t.Helper()
	result, err := w.plan(repo)
	if err != nil || result.Drift == nil {
		w.t.Fatalf("Plan() = %+v, %v, want the drift of a created sandbox", result, err)
	}
	return *result.Drift
}

func paths(comparison Comparison) []string {
	var found []string
	for _, difference := range comparison.Differences {
		found = append(found, difference.Path)
	}
	return found
}

func TestAddingAnEgressLineToTheRepoDeclarationIsDriftAndCreateGoesNoFurther(t *testing.T) {
	w := newWorld(t, testUserConfig)
	repo := localRepo(t, "app", repoWithEgress)
	w.mustCreate(repo)
	if err := os.WriteFile(filepath.Join(repo, repoDeclarationFile), []byte(repoWithEgress+"  more:\n    rationale: test\n    allow: [more.example.com:443]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gate := approvedByHuman()

	result, err := w.create(repo, gate)

	if err == nil || result.Drift == nil || len(result.Drift.Differences) != 1 || !strings.Contains(result.Drift.Differences[0].Current, "more.example.com:443") {
		t.Errorf("Create() = %+v, %v, want the sandbox_egress drift", result, err)
	}
	if len(gate.proposals) != 0 || len(w.vms.Sandbox("app").EgressRules) != 1 {
		t.Errorf("proposals = %d, rules = %q, want neither the gate nor any change", len(gate.proposals), w.vms.Sandbox("app").EgressRules)
	}
}

func TestAddingAnEgressLineToTheUserConfigIsNotDrift(t *testing.T) {
	w := newWorld(t, testUserConfig)
	repo := localRepo(t, "app", "")
	w.mustCreate(repo)
	w.writeUserConfig(testUserConfig + "egress:\n  pypi:\n    rationale: PyPI\n    allow: [pypi.org:443]\n")

	if drift := w.driftOf(repo); len(drift.Differences) != 0 {
		t.Errorf("drift = %+v, want none from a global rule", drift)
	}
}

func TestTheCurrentDeclarationOfAGitURLIsReadFromAFreshClone(t *testing.T) {
	w := newWorld(t, testUserConfig)
	w.mustCreate(appURL)
	w.clonedRepoDecl = "version: 1\nprofile:\n  model: sonnet\n"

	if drift := w.driftOf(appURL); !strings.Contains(strings.Join(paths(drift), " "), "profile.model") {
		t.Errorf("drift = %+v, want the model at the new HEAD", drift)
	}
}

func TestChangingTheHostsOfASecretDefinitionIsDrift(t *testing.T) {
	const gitlab = testUserConfig + "secrets: [gitlab]\negress:\n  gitlab:\n    rationale: GitLab\n    allow: [a.example.com:443, b.example.com:443]\n"
	w := newWorld(t, gitlab+"secret_defs:\n  gitlab:\n    key: GITLAB_TOKEN\n    hosts: [a.example.com]\n    env: GITLAB_TOKEN\n")
	w.secrets = secret.Values{"GITLAB_TOKEN": "glpat_x"}
	repo := localRepo(t, "app", "")
	w.mustCreate(repo)
	w.writeUserConfig(gitlab + "secret_defs:\n  gitlab:\n    key: GITLAB_TOKEN\n    hosts: [b.example.com]\n    env: GITLAB_TOKEN\n")

	if drift := w.driftOf(repo); len(drift.Differences) != 1 || drift.Differences[0].Path != "secrets" || !strings.Contains(drift.Differences[0].Current, "b.example.com") {
		t.Errorf("drift = %+v, want the wired secrets", drift)
	}
}

// gitlabSecret は vars を持つ placeholder 注入の secret を配線する user 設定。defs は secret_defs.gitlab の中身。
func gitlabSecret(defs string) string {
	return testUserConfig + "secrets: [gitlab]\negress:\n  gitlab:\n    rationale: GitLab\n    allow: [gitlab.example.com:443]\n" +
		"secret_defs:\n  gitlab:\n    hosts: [gitlab.example.com]\n    env: GITLAB_TOKEN\n" + defs
}

const gitlabTokenWithHost = "    key: GITLAB_TOKEN\n    vars:\n      GITLAB_HOST: gitlab.example.com\n"

func TestChangingTheVarsOrKeyOfASecretDefinitionIsDrift(t *testing.T) {
	for _, tc := range []struct{ name, defs, want string }{
		{"vars", "    key: GITLAB_TOKEN\n    vars:\n      GITLAB_HOST: git.example.com\n", "git.example.com"},
		{"key", "    key: GITLAB_PAT\n    vars:\n      GITLAB_HOST: gitlab.example.com\n", "GITLAB_PAT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, gitlabSecret(gitlabTokenWithHost))
			w.secrets = secret.Values{"GITLAB_TOKEN": "glpat_x"}
			repo := localRepo(t, "app", "")
			w.mustCreate(repo)
			w.writeUserConfig(gitlabSecret(tc.defs))

			drift := w.driftOf(repo)
			_, err := w.create(repo, unattended())

			if len(drift.Differences) != 1 || drift.Differences[0].Path != "secrets" || !strings.Contains(drift.Differences[0].Current, tc.want) {
				t.Errorf("drift = %+v, want the secrets difference with %q now", drift, tc.want)
			}
			if err == nil {
				t.Errorf("Create() error = nil, want create to stop on the drift")
			}
		})
	}
}

func TestReorderingRequestedSecretsIsNotDrift(t *testing.T) {
	const defs = "egress:\n  hosts:\n    rationale: test\n    allow: [a.example.com:443, b.example.com:443]\n" +
		"secret_defs:\n  one:\n    key: ONE\n    hosts: [a.example.com]\n    env: ONE\n  two:\n    key: TWO\n    hosts: [b.example.com]\n    env: TWO\n"
	w := newWorld(t, testUserConfig+"secrets: [one, two]\n"+defs)
	w.secrets = secret.Values{"ONE": "1", "TWO": "2"}
	repo := localRepo(t, "app", "")
	w.mustCreate(repo)
	w.writeUserConfig(testUserConfig + "secrets: [two, one]\n" + defs)

	if drift := w.driftOf(repo); len(drift.Differences) != 0 {
		t.Errorf("drift = %+v, want the same wired secrets in another order to be no drift", drift)
	}
}

func TestPlanOfASandboxRecordedWithoutSecretKeysNotesItInsteadOfShowingDrift(t *testing.T) {
	w := newWorld(t, gitlabSecret(gitlabTokenWithHost))
	w.secrets = secret.Values{"GITLAB_TOKEN": "glpat_x"}
	repo := localRepo(t, "app", "")
	w.mustCreate(repo)
	// v0.1.0 が書いた作成時の宣言は、配線した secret の key と vars を持たない
	w.rewriteRecordedSecrets(func(wired map[string]any) {
		delete(wired, "key")
		delete(wired, "vars")
	})

	drift := w.driftOf(repo)

	if len(drift.Differences) != 0 || len(drift.NotCompared) != 1 || !strings.Contains(drift.NotCompared[0], "gitlab") {
		t.Errorf("drift = %+v, want no drift and a note that the key and vars of gitlab were not compared", drift)
	}
}

// rewriteRecordedSecrets は状態ディレクトリの作成時の宣言の、配線した secret を 1 つずつ書き換える。
func (w *world) rewriteRecordedSecrets(rewrite func(wired map[string]any)) {
	w.t.Helper()
	dir := w.places.stateDirOf("app")
	data, _, err := dir.read(declarationFile)
	if err != nil {
		w.t.Fatal(err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		w.t.Fatal(err)
	}
	for _, wired := range tree["secrets"].([]any) {
		rewrite(wired.(map[string]any))
	}
	if data, err = yaml.Marshal(tree); err == nil {
		err = dir.write(declarationFile, data)
	}
	if err != nil {
		w.t.Fatal(err)
	}
}

func TestPlanDoesNotAskTheRuntime(t *testing.T) {
	w := newWorld(t, testUserConfig)
	repo := localRepo(t, "app", "")
	w.mustCreate(repo)
	lc := w.lifecycle()
	lc.Runtime = nil // 実行基盤に問い合わせれば panic する

	result, err := lc.Plan(context.Background(), repo)

	if err != nil || result.Drift == nil {
		t.Errorf("Plan() = %+v, %v, want the drift read from the state dir alone", result, err)
	}
}
