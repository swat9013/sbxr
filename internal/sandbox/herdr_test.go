package sandbox

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/swat9013/sbxr/internal/assets"
	"github.com/swat9013/sbxr/internal/runtime"
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
	saved := kitWait
	t.Cleanup(func() { kitWait = saved })
	kitWait.budget, kitWait.interval, kitWait.sleep = 3*time.Second, time.Second, func(time.Duration) {}
	fake := &sbxstub.FakeVM{Files: map[string]string{kitStartupLog: "=== dispatcher run ===\n> /etc/durable-startup.d/002-startup-sbxr-herdr/000-cmd.sh\n"}}
	stub := &sbxstub.Stub{Sandboxes: map[string]string{"app": "running"}, VM: fake}

	err := waitKitStartup(context.Background(), runtime.NewSbx(stub.Run), "app")

	if err == nil || !strings.Contains(err.Error(), "終わらない") {
		t.Errorf("waitKitStartup() error = %v, want a timeout", err)
	}
}
