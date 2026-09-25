package sandbox

import "testing"

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
