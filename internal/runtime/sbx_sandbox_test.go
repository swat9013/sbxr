package runtime

import (
	"context"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/swat9013/sbxr/internal/assets"
	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

func TestStartupOutcomeReadsOnlyTheLatestDispatcherRun(t *testing.T) {
	failedThenComplete := "=== dispatcher run 1 ===\nfail /etc/durable-startup.d/002-startup-sbxr-herdr/000-cmd.sh exit=1\n" +
		"=== dispatcher run 2 ===\nok /etc/durable-startup.d/002-startup-sbxr-herdr/000-cmd.sh\n=== dispatcher complete ===\n"
	completeThenRunning := "=== dispatcher run 1 ===\n=== dispatcher complete ===\n=== dispatcher run 2 ===\n> /etc/durable-startup.d/002-startup-sbxr-herdr/000-cmd.sh\n"
	for _, tc := range []struct {
		name, log string
		want      startup
	}{
		{"no log yet", "", startupRunning},
		{"a failure in the latest run", "=== dispatcher run 1 ===\nfail /etc/durable-startup.d/003-startup-sbxr-boot/000-cmd.sh exit=1\n", startupFailed},
		{"command output that starts with fail", "=== dispatcher run 1 ===\nfail to resolve host, retrying\n=== dispatcher complete ===\n", startupComplete},
		{"an earlier failure is not the latest run", failedThenComplete, startupComplete},
		{"an earlier completion is not the latest run", completeThenRunning, startupRunning},
	} {
		if got := startupOutcome(tc.log); got != tc.want {
			t.Errorf("%s: startupOutcome = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestTheHerdrKitReportsEachFailureWithTheLineSbxrReads(t *testing.T) {
	spec, err := fs.ReadFile(assets.Kits(), herdrKit+"/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}

	for _, stage := range []string{"install", "server", "integration"} {
		if !strings.Contains(string(spec), `echo "`+herdrKitFailure+stage) {
			t.Errorf("herdr kit has no %q line for the %s stage", herdrKitFailure+stage, stage)
		}
	}
}

func TestWaitingForKitStartupGivesUpAfterTheBudget(t *testing.T) {
	saved := startupWait
	t.Cleanup(func() { startupWait = saved })
	startupWait.Budget, startupWait.Interval, startupWait.Sleep = 3*time.Second, time.Second, func(time.Duration) {}
	fake := &sbxstub.FakeVM{Files: map[string]string{kitStartupLog: "=== dispatcher run ===\n> /etc/durable-startup.d/002-startup-sbxr-herdr/000-cmd.sh\n"}}
	stub := &sbxstub.Stub{Sandboxes: map[string]string{"app": "running"}, VM: fake}

	err := NewSbx(stub.Run).waitStartup(context.Background(), "app")

	if err == nil || !strings.Contains(err.Error(), "終わらない") {
		t.Errorf("waitStartup() error = %v, want a timeout", err)
	}
}

func TestTheBootKitRunsTheScriptWhereSbxrWritesIt(t *testing.T) {
	spec, err := fs.ReadFile(assets.Kits(), bootKit+"/spec.yaml")

	if err != nil || !strings.Contains(string(spec), `"$HOME/`+BootScriptRelPath+`"`) {
		t.Errorf("sbxr-boot spec = %q, %v, want it to run $HOME/%s", spec, err, BootScriptRelPath)
	}
}

func TestAFailedSecretIsNamedWithTheSecretsAlreadyPlaced(t *testing.T) {
	stub := &sbxstub.Stub{FailOn: "secret set-custom"}
	sbx := NewSbx(stub.Run)
	dir := t.TempDir()
	spec := SandboxSpec{Name: "app", Repo: "/src/app", Secrets: []SandboxSecret{
		{Service: "github", Value: "v"},
		{Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Value: "v"},
	}}
	if err := sbx.DefineSandbox(dir, spec); err != nil {
		t.Fatal(err)
	}

	err := sbx.CreateSandbox(context.Background(), dir, spec)

	if err == nil || !strings.Contains(err.Error(), "secret env GITLAB_TOKEN を置けない") || !strings.Contains(err.Error(), "置いた分: [service github]") {
		t.Errorf("CreateSandbox() error = %v, want the failed secret and the placed ones", err)
	}
}

func TestTheEnvDefinitionCarriesTheBootKitOnlyWhenBootIsReplayed(t *testing.T) {
	for _, replay := range []bool{true, false} {
		dir := t.TempDir()

		if err := NewSbx((&sbxstub.Stub{}).Run).DefineSandbox(dir, SandboxSpec{Name: "app", Repo: "/src/app", ReplayBoot: replay}); err != nil {
			t.Fatal(err)
		}

		env, _, err := readEnvDefinition(dir)
		if err != nil {
			t.Fatal(err)
		}
		carries := slices.ContainsFunc(env.Kits, func(k envKit) bool { return k.Source == "./kits/"+bootKit })
		if carries != replay {
			t.Errorf("ReplayBoot %v: env kits = %+v, want the boot kit %v", replay, env.Kits, replay)
		}
	}
}
