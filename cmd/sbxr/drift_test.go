package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- 既存 VM への create と drift ---

func TestCreateOnAnExistingSandboxWithoutDeclarationChangesReportsNoDriftAndSucceeds(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", repoWithEgress)
	lc.mustRun(t, "create", repo, "--yes")

	out := lc.mustRun(t, "create", repo, "--yes")

	if !strings.Contains(out, "既にある") || !strings.Contains(out, "差分は無い") {
		t.Errorf("output = %q, want it to say the sandbox exists without drift", out)
	}
}

func TestAddingAnEgressLineToTheRepoDeclarationIsDrift(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", repoWithEgress)
	lc.mustRun(t, "create", repo, "--yes")
	writeRepoDecl(t, repo, repoWithEgress+"  more:\n    rationale: test\n    allow: [more.example.com:443]\n")
	writes := len(lc.stub.Writes)

	out, err := lc.run(t, "create", repo)

	if err == nil {
		t.Fatalf("create succeeded, want a non-zero exit for drift; output = %q", out)
	}
	if !strings.Contains(out, "sandbox_egress") || !strings.Contains(out, "more.example.com:443") {
		t.Errorf("output = %q, want the sandbox_egress difference", out)
	}
	if !strings.Contains(err.Error(), "sbxr destroy "+repo) || !strings.Contains(err.Error(), "sbxr create "+repo) {
		t.Errorf("error = %v, want the destroy → create steps", err)
	}
	if len(lc.prompter.prompts) != 0 || len(lc.stub.Writes) != writes {
		t.Errorf("prompts = %v, writes = %q, want neither the gate nor any change", lc.prompter.prompts, lc.stub.Writes[writes:])
	}
}

func TestAddingAnEgressLineToTheUserConfigIsNotDrift(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	writeUserConfig(t, lc, lifecycleUserConfig+"egress:\n  pypi:\n    rationale: PyPI\n    allow: [pypi.org:443]\n")

	out := lc.mustRun(t, "create", repo, "--yes")

	if !strings.Contains(out, "差分は無い") {
		t.Errorf("output = %q, want no drift from a global rule", out)
	}
}

func TestASandboxFromAGitURLWithYesHasNoDriftWhileTheDeclarationIsUnchanged(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.clonedRepoDecl = repoWithEgress
	lc.mustRun(t, "create", "https://example.com/me/app.git", "--yes")

	out := lc.mustRun(t, "create", "https://example.com/me/app.git", "--yes")

	if !strings.Contains(out, "差分は無い") {
		t.Errorf("output = %q, want the dropped repo egress to be dropped again when comparing", out)
	}
}

func TestTheCurrentDeclarationOfAGitURLIsReadFromAFreshClone(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.mustRun(t, "create", "https://example.com/me/app.git", "--yes")
	lc.clonedRepoDecl = "version: 1\nprofile:\n  model: sonnet\n"

	out, err := lc.run(t, "create", "https://example.com/me/app.git", "--yes")

	if err == nil || !strings.Contains(out, "profile.model") {
		t.Errorf("error = %v, output = %q, want drift from the declaration at the new HEAD", err, out)
	}
}

func TestChangingTheHostsOfASecretDefinitionIsDrift(t *testing.T) {
	const gitlab = lifecycleUserConfig + "secrets: [gitlab]\negress:\n  gitlab:\n    rationale: GitLab\n    allow: [a.example.com:443, b.example.com:443]\n"
	lc := newLifecycle(t, gitlab+"secret_defs:\n  gitlab:\n    key: GITLAB_TOKEN\n    hosts: [a.example.com]\n    env: GITLAB_TOKEN\n")
	secretFile, _ := lc.deps.secretFilePath()
	if err := os.WriteFile(secretFile, []byte("GITLAB_TOKEN=glpat_x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	writeUserConfig(t, lc, gitlab+"secret_defs:\n  gitlab:\n    key: GITLAB_TOKEN\n    hosts: [b.example.com]\n    env: GITLAB_TOKEN\n")

	out, err := lc.run(t, "create", repo, "--yes")

	if err == nil || !strings.Contains(out, "secrets") || !strings.Contains(out, "b.example.com") {
		t.Errorf("error = %v, output = %q, want drift in the wired secrets", err, out)
	}
}

func TestPlanOfAnExistingSandboxShowsTheDrift(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	writeRepoDecl(t, repo, "version: 1\ninit: [make setup]\n")

	out := lc.mustRun(t, "plan", repo)

	if !strings.Contains(out, "差分が 1 箇所") || !strings.Contains(out, "  init\n    作成時: []\n    現在:   [\"make setup\"]") {
		t.Errorf("output = %q, want the init difference", out)
	}
}

func writeRepoDecl(t *testing.T, repo, decl string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "sbxr.yaml"), []byte(decl), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeUserConfig(t *testing.T, lc *lifecycle, config string) {
	t.Helper()
	if err := os.WriteFile(lc.places.UserConfig, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckingDriftOfAGitURLLeavesTheCacheCloneOfTheSandboxAlone(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.mustRun(t, "create", "https://example.com/me/app.git", "--yes")
	fetched := filepath.Join(lc.places.CacheRoot, "app", "fetched-from-the-vm")
	if err := os.WriteFile(fetched, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	lc.mustRun(t, "create", "https://example.com/me/app.git", "--yes")

	if !exists(fetched) {
		t.Errorf("the cache clone was replaced, want it kept (it carries the sandbox's git remote and fetched commits)")
	}
}

func TestPlanDoesNotNeedSbx(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	lc.stub.FailOn = "ls"

	out := lc.mustRun(t, "plan", repo)

	if !strings.Contains(out, "差分は無い") {
		t.Errorf("output = %q, want the drift read from the state dir alone", out)
	}
}

func TestReorderingRequestedSecretsIsNotDrift(t *testing.T) {
	const defs = "egress:\n  hosts:\n    rationale: test\n    allow: [a.example.com:443, b.example.com:443]\n" +
		"secret_defs:\n  one:\n    key: ONE\n    hosts: [a.example.com]\n    env: ONE\n  two:\n    key: TWO\n    hosts: [b.example.com]\n    env: TWO\n"
	lc := newLifecycle(t, lifecycleUserConfig+"secrets: [one, two]\n"+defs)
	secretFile, _ := lc.deps.secretFilePath()
	if err := os.WriteFile(secretFile, []byte("ONE=1\nTWO=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	writeUserConfig(t, lc, lifecycleUserConfig+"secrets: [two, one]\n"+defs)

	out := lc.mustRun(t, "create", repo, "--yes")

	if !strings.Contains(out, "差分は無い") {
		t.Errorf("output = %q, want the same wired secrets in another order to be no drift", out)
	}
}

func TestCreateOnAnExistingSandboxWhoseRepoIsGoneSaysTheSandboxExists(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}

	_, err := lc.run(t, "create", repo, "--yes")

	if err == nil || !strings.Contains(err.Error(), "既にある") {
		t.Errorf("error = %v, want it to say the sandbox exists but the drift cannot be checked", err)
	}
}

func TestPlanOfASandboxFromAGitURLWithYesStillSummarizesTheRepoEgress(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.clonedRepoDecl = repoWithEgress
	lc.mustRun(t, "create", "https://example.com/me/app.git", "--yes")

	out := lc.mustRun(t, "plan", "https://example.com/me/app.git")

	if !strings.Contains(out, "api.example.com:443") || !strings.Contains(out, "差分は無い") {
		t.Errorf("output = %q, want the repo egress in the summary and no drift", out)
	}
}
