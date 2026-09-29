package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime"
)

// doctorItem は doctor の出力から、名前が name の項目の行 ("  fail  user 設定: ...") を返す。無ければ空。
func doctorItem(out, name string) string {
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		_, item, _ := strings.Cut(line, " ")
		if strings.HasPrefix(strings.TrimSpace(item), name+":") {
			return line
		}
	}
	return ""
}

func assertDoctorItem(t *testing.T, out, status, name string) {
	t.Helper()
	line := doctorItem(out, name)
	if line == "" {
		t.Errorf("doctor output has no item %q, output = %q", name, out)
		return
	}
	if got := strings.Fields(line)[0]; got != status {
		t.Errorf("doctor item %q = %q, want %s", name, line, status)
	}
}

func writeSecretFile(t *testing.T, lc *lifecycle, content string, mode os.FileMode) {
	t.Helper()
	path, _ := lc.deps.secretFilePath()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // umask に削られない mode にする
		t.Fatal(err)
	}
}

func TestDoctorReportsUserAndRepoErrorsInOneRun(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+"profile:\n  modle: sonnet\n")
	repo := localRepo(t, "app", "version: 1\nherdr:\n  enabled: true\n")

	out, err := lc.run(t, "doctor", repo)

	if err == nil {
		t.Errorf("doctor error = nil, want a non-zero exit when an item fails")
	}
	assertDoctorItem(t, out, "fail", "user 設定")
	assertDoctorItem(t, out, "fail", "repo 宣言")
	if !strings.Contains(out, "profile.modle") || !strings.Contains(out, "herdr は repo 宣言には書けない") {
		t.Errorf("output = %q, want both the user's unknown key and the repo's restricted key named", out)
	}
	if len(lc.stub.Writes) != 0 {
		t.Errorf("sbx writes = %q, want doctor to change nothing", lc.stub.Writes)
	}
}

func TestDoctorSkipsTheGlobalRuleCheckWhenSbxIsMissingAndChecksTheRest(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+"secrets: [github]\nherdr:\n  enabled: true\n")
	writeSecretFile(t, lc, "GITHUB_TOKEN=ghp_x\n", 0o600)
	lc.deps.sbxAvailable = func() error { return errors.New("sbx が PATH に無い") }
	lc.deps.runtime = runtime.NewSbx(func(_ context.Context, _ io.Reader, args ...string) ([]byte, error) {
		t.Errorf("sbx %q was run, want no sbx call when sbx is missing", args)
		return nil, errors.New("no sbx")
	})
	repo := localRepo(t, "app", repoWithEgress)

	out, err := lc.run(t, "doctor", repo)

	if err == nil {
		t.Errorf("doctor error = nil, want a non-zero exit for the missing sbx")
	}
	assertDoctorItem(t, out, "fail", "sbx")
	assertDoctorItem(t, out, "skip", "global rule")
	for _, name := range []string{"secret ファイル", "herdr", "user 設定", "repo 宣言", "repo の egress", "git identity", "secret github"} {
		assertDoctorItem(t, out, "ok", name)
	}
}

func TestDoctorShowsTheSetupCommandWhenARequestedSecretHasNoValue(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+"secrets: [github]\n")

	out, err := lc.run(t, "doctor")

	if err == nil {
		t.Errorf("doctor error = nil, want a non-zero exit when a secret value is missing")
	}
	assertDoctorItem(t, out, "fail", "secret github")
	if !strings.Contains(out, "sbxr secret setup github") {
		t.Errorf("output = %q, want the command that writes the value", out)
	}
}

func TestDoctorExitsZeroWhenEveryItemPasses(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+"secrets: [github]\nherdr:\n  enabled: true\n")
	writeSecretFile(t, lc, "GITHUB_TOKEN=ghp_x\n", 0o600)
	lc.mustRun(t, "policy", "sync")
	syncWrites := len(lc.stub.Writes)
	repo := localRepo(t, "app", repoWithEgress)

	out, err := lc.run(t, "doctor", repo)

	if err != nil {
		t.Fatalf("doctor error = %v, want 0 when every item is ok, output = %q", err, out)
	}
	for _, status := range []string{"fail", "skip"} {
		for line := range strings.Lines(out) {
			if strings.HasPrefix(strings.TrimSpace(line), status+" ") {
				t.Errorf("doctor item %q, want every item ok", strings.TrimSpace(line))
			}
		}
	}
	for _, name := range []string{"sbx", "secret ファイル", "herdr", "user 設定", "global rule", "repo 宣言", "repo の egress", "git identity", "secret github"} {
		assertDoctorItem(t, out, "ok", name)
	}
	if len(lc.stub.Writes) != syncWrites {
		t.Errorf("sbx writes = %q, want doctor to add none", lc.stub.Writes[syncWrites:])
	}
}

func TestDoctorFailsWhenTheGlobalRulesDifferFromTheDeclaration(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)

	out, err := lc.run(t, "doctor")

	if err == nil {
		t.Errorf("doctor error = nil, want a non-zero exit for global rules out of sync")
	}
	assertDoctorItem(t, out, "fail", "global rule")
	if !strings.Contains(out, "sbxr policy sync") {
		t.Errorf("output = %q, want policy sync shown as the fix", out)
	}
}

func TestDoctorSkipsTheSecretValuesWhenTheSecretFileIsUnreadable(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+"secrets: [github]\n")
	writeSecretFile(t, lc, "GITHUB_TOKEN=ghp_x\n", 0o644)

	out, _ := lc.run(t, "doctor")

	assertDoctorItem(t, out, "fail", "secret ファイル")
	assertDoctorItem(t, out, "skip", "secret github")
	if !strings.Contains(out, "chmod 600") {
		t.Errorf("output = %q, want chmod 600 shown as the fix", out)
	}
}

func TestDoctorSkipsWhatDependsOnAnInvalidUserConfigButChecksTheRepoFile(t *testing.T) {
	lc := newLifecycle(t, "version: 2\n")
	repo := localRepo(t, "app", repoWithEgress)

	out, _ := lc.run(t, "doctor", repo)

	assertDoctorItem(t, out, "fail", "user 設定")
	for _, name := range []string{"herdr", "global rule", "git identity", "secret"} {
		assertDoctorItem(t, out, "skip", name)
	}
	for _, name := range []string{"repo 宣言", "repo の egress"} {
		assertDoctorItem(t, out, "ok", name)
	}
}

func TestDoctorFailsASecretWhoseHostTheEgressDoesNotAllow(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+`secret_defs:
  api:
    key: API_TOKEN
    hosts: [api.example.com]
    env: API_TOKEN
`)
	writeSecretFile(t, lc, "API_TOKEN=x\n", 0o600)
	repo := localRepo(t, "app", "version: 1\nsecrets: [api]\n")

	out, _ := lc.run(t, "doctor", repo)

	assertDoctorItem(t, out, "fail", "secret api")
	if !strings.Contains(out, "api.example.com") {
		t.Errorf("output = %q, want the host the egress does not allow", out)
	}
}

func TestDoctorWithoutARepoSkipsASecretThatOnlyARepoEgressCouldWire(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+`secrets: [api]
secret_defs:
  api:
    key: API_TOKEN
    hosts: [api.example.com]
    env: API_TOKEN
`)

	out, _ := lc.run(t, "doctor")

	assertDoctorItem(t, out, "skip", "secret api")
}

func TestDoctorReadsTheRepoDeclarationOfAGitURLFromATemporaryClone(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.clonedRepoDecl = "version: 1\negress:\n  api:\n    allow: [api.example.com:443]\n"

	out, _ := lc.run(t, "doctor", "https://example.com/me/app.git")

	assertDoctorItem(t, out, "ok", "repo 宣言")
	if line := doctorItem(out, "repo 宣言"); strings.Contains(line, "は無い") {
		t.Errorf("doctor item %q, want the cloned sbxr.yaml reported as read", line)
	}
	assertDoctorItem(t, out, "fail", "repo の egress")
	if len(lc.clones) != 1 {
		t.Errorf("clones = %q, want the git URL cloned once", lc.clones)
	}
	if exists(lc.places.CacheRoot) {
		t.Errorf("cache root %s exists, want doctor to leave no cache clone", lc.places.CacheRoot)
	}
}

func TestDoctorChecksTheUserSecretValuesEvenWhenTheRepoDeclarationFails(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+"secrets: [github]\n")
	repo := localRepo(t, "app", "version: 1\ninit: make\n")

	out, _ := lc.run(t, "doctor", repo)

	assertDoctorItem(t, out, "fail", "repo 宣言")
	assertDoctorItem(t, out, "skip", "git identity")
	assertDoctorItem(t, out, "fail", "secret github")
}

func TestDoctorFailsWhenHerdrIsEnabledButMissingOnTheHost(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+"herdr:\n  enabled: true\n")
	lc.herdr.Missing = true

	out, _ := lc.run(t, "doctor")

	assertDoctorItem(t, out, "fail", "herdr")
}

func TestDoctorFailsWhenNoScopeDeclaresTheGitIdentity(t *testing.T) {
	lc := newLifecycle(t, "version: 1\n")
	repo := localRepo(t, "app", "version: 1\n")

	out, _ := lc.run(t, "doctor", repo)

	assertDoctorItem(t, out, "fail", "git identity")
	if !strings.Contains(out, "repo 宣言 (sbxr.yaml) に git.name") {
		t.Errorf("output = %q, want the repo declaration offered as a place for the git identity", out)
	}
}

func TestDoctorFailsASecretFileLineThatIsNotKeyEqualsValue(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	writeSecretFile(t, lc, "GITHUB_TOKEN\n", 0o600)

	out, _ := lc.run(t, "doctor")

	assertDoctorItem(t, out, "fail", "secret ファイル")
	if !strings.Contains(out, "KEY=VALUE の形に直す") {
		t.Errorf("output = %q, want the line format shown as the fix", out)
	}
}

func TestDoctorFailsARepoThatIsNotADirectory(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)

	out, _ := lc.run(t, "doctor", filepath.Join(t.TempDir(), "missing"))

	assertDoctorItem(t, out, "fail", "repo 宣言")
	assertDoctorItem(t, out, "skip", "repo の egress")
}

func TestDoctorFailsSecretsWhoseVarsDisagree(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+`secrets: [a, b]
secret_defs:
  a:
    key: A_TOKEN
    hosts: [github.com]
    env: A_TOKEN
    vars: {REGION: east}
  b:
    key: B_TOKEN
    hosts: [github.com]
    env: B_TOKEN
    vars: {REGION: west}
`)

	out, _ := lc.run(t, "doctor")

	assertDoctorItem(t, out, "fail", "secret の vars")
}

func TestDoctorTellsToWriteTheSecretFileWhenNoSetupCommandWritesTheKey(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+`secrets: [other]
secret_defs:
  other:
    service: other
    key: OTHER_TOKEN
    hosts: [github.com]
`)

	out, _ := lc.run(t, "doctor")

	assertDoctorItem(t, out, "fail", "secret other")
	if !strings.Contains(out, "OTHER_TOKEN=<値> の行を書く") {
		t.Errorf("output = %q, want writing the secret file shown as the fix", out)
	}
}
