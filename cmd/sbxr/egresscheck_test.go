package main

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
	"github.com/swat9013/sbxr/internal/sandbox"
)

// requireEgressCheckFailure は create が egress 自己検証で止まり、VM を残して作成時の宣言を書かなかったことを確かめる。
func requireEgressCheckFailure(t *testing.T, lc *lifecycle, repo string, err error, wantMessage string) {
	t.Helper()
	var stageErr *sandbox.StageError
	if !errors.As(err, &stageErr) || stageErr.Stage != sandbox.StageEgressCheck {
		t.Fatalf("error = %v, want an egress self-check failure", err)
	}
	if !strings.Contains(err.Error(), wantMessage) {
		t.Errorf("error = %v, want it to mention %q", err, wantMessage)
	}
	if _, ok := lc.stub.Sandboxes["app"]; !ok {
		t.Errorf("sandbox was removed; the VM is kept for inspection")
	}
	// 作成が終わった印はまだ無い (自己検証の後に書く) ので、作り直しは途中で止まった VM として拒まれる
	if _, err := lc.run(t, "create", repo, "--yes"); err == nil || !strings.Contains(err.Error(), "途中で止まっている") {
		t.Errorf("create again error = %v, want the creation counted as unfinished", err)
	}
}

func TestCreatePassesTheEgressSelfCheckAndReportsBothProbes(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := gitRepo(t, "app", "", "https://github.com/me/app.git")

	out := lc.mustRun(t, "create", repo, "--yes")

	for _, want := range []string{"許可先 astral.sh:443 に届いた", "許可外の example.com:443 は proxy が拒否した"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want %q", out, want)
		}
	}
}

func TestCreateCountsTheAllowedHostsOwnErrorStatusAsReached(t *testing.T) {
	for _, result := range []sbxstub.HTTPResult{{Code: "404", Body: "not found"}, {Code: "403", Body: "Forbidden"}, {Code: "301"}} {
		t.Run(result.Code+" "+result.Body, func(t *testing.T) {
			lc := newLifecycle(t, lifecycleUserConfig)
			lc.stub.VM.HTTP = map[string]sbxstub.HTTPResult{"astral.sh": result}
			repo := gitRepo(t, "app", "", "https://github.com/me/app.git")

			lc.mustRun(t, "create", repo, "--yes")
		})
	}
}

func TestCreateStopsWhenTheProxyDeniesTheAllowedHost(t *testing.T) {
	lc := herdrLifecycle(t)
	lc.stub.VM.HTTP = map[string]sbxstub.HTTPResult{"astral.sh": sbxstub.ProxyDenial("astral.sh")}
	repo := gitRepo(t, "app", "", "https://github.com/me/app.git")

	_, err := lc.run(t, "create", repo, "--yes")

	requireEgressCheckFailure(t, lc, repo, err, "許可先 astral.sh:443 に届かない")
	if len(lc.herdr.Machines) != 0 {
		t.Errorf("herdr machines = %v, want none registered for an unfinished creation", lc.herdr.Machines)
	}
}

func TestCreateStopsWhenTheAllowedHostGivesNoResponse(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.stub.VM.HTTP = map[string]sbxstub.HTTPResult{"astral.sh": {Body: "curl: (28) Connection timed out"}}
	repo := gitRepo(t, "app", "", "https://github.com/me/app.git")

	_, err := lc.run(t, "create", repo, "--yes")

	requireEgressCheckFailure(t, lc, repo, err, "Connection timed out")
}

func TestCreateStopsWhenTheDisallowedHostIsNotDeniedByTheProxy(t *testing.T) {
	for name, result := range map[string]sbxstub.HTTPResult{
		"宛先の応答":                    {Code: "200", Body: "<html>"},
		"proxy の文言の無い 403":         {Code: "403", Body: "Forbidden"},
		"proxy の拒否応答を得ずに curl が失敗": {Body: "curl: (6) Could not resolve host: example.com"},
	} {
		t.Run(name, func(t *testing.T) {
			lc := newLifecycle(t, lifecycleUserConfig)
			lc.stub.VM.HTTP = map[string]sbxstub.HTTPResult{"example.com": result}
			repo := gitRepo(t, "app", "", "https://github.com/me/app.git")

			_, err := lc.run(t, "create", repo, "--yes")

			requireEgressCheckFailure(t, lc, repo, err, "許可外の example.com:443")
		})
	}
}

func TestCreateProbesTheNextDisallowedCandidateWhenExampleComIsAllowed(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+"egress:\n  mine:\n    rationale: test\n    allow:\n      - example.com:443\n")
	repo := gitRepo(t, "app", "", "https://github.com/me/app.git")

	lc.mustRun(t, "create", repo, "--yes")

	if !slices.Contains(lc.stub.VM.Events, "probe https://example.net/") || slices.Contains(lc.stub.VM.Events, "probe https://example.com/") {
		t.Errorf("VM events = %q, want example.net probed instead of the allowed example.com", lc.stub.VM.Events)
	}
}

// repoEgressFirstInOrder は、文字列順で既定の宣言のどの許可先よりも前に来る宛先を repo の egress に持つ repo 宣言。
const repoEgressFirstInOrder = "version: 1\negress:\n  api:\n    rationale: test\n    allow:\n      - aaa.example.net:443\n"

func TestCreateCountsTheRepoEgressInTheAllowSet(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := gitRepo(t, "app", repoEgressFirstInOrder, "https://github.com/me/app.git")

	lc.mustRun(t, "create", repo, "--yes")

	if !slices.Contains(lc.stub.VM.Events, "probe https://aaa.example.net/") {
		t.Errorf("VM events = %q, want the sandbox scope destination picked as the allowed host", lc.stub.VM.Events)
	}
}

func TestCreateLeavesTheDroppedRepoEgressOutOfTheAllowSet(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.clonedRepoDecl = repoEgressFirstInOrder

	lc.mustRun(t, "create", "https://example.com/me/app.git", "--yes")

	if want := defaultEgressProbes; !slices.Equal(lc.stub.VM.Events, want) {
		t.Errorf("VM events = %q, want %q (the dropped repo egress is not allowed in the VM)", lc.stub.VM.Events, want)
	}
}
