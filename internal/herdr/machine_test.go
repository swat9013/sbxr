package herdr_test

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/herdr"
	"github.com/swat9013/sbxr/internal/herdr/herdrtest"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/inmemory"
)

// runningApp は稼働中の sandbox VM app を持つ実行基盤。
func runningApp() *inmemory.Runtime {
	rt := inmemory.New()
	rt.Sandbox("app").Status = runtime.SandboxRunning
	return rt
}

func TestRegisterStopsTheServerInTheVMBeforeAddingTheMachine(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{}
	machines := herdr.Machines{Client: host, VM: rt}

	err := machines.Register(context.Background(), "app", io.Discard)

	if err != nil || !slices.Equal(host.Calls, []string{"list", "add " + rt.SSHTarget("app") + " app"}) {
		t.Errorf("Register() = %v, herdr calls = %v, want the machine added", err, host.Calls)
	}
	if len(rt.Commands) != 1 || !strings.Contains(strings.Join(rt.Commands[0].Args, " "), "pkill -x herdr") {
		t.Errorf("VM commands = %v, want the herdr server stopped first", rt.Commands)
	}
}

func TestRegisterLeavesAStaleRegistrationAloneAndShowsHowToReplaceIt(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{Machines: []herdr.Machine{{ID: "old", Target: rt.SSHTarget("app")}}}
	machines := herdr.Machines{Client: host, VM: rt}

	err := machines.Register(context.Background(), "app", io.Discard)

	var registration *herdr.RegistrationError
	if !errors.As(err, &registration) || !strings.HasPrefix(registration.Recovery, "herdr machine remove old; ") {
		t.Errorf("Register() = %v, want the stale registration and how to replace it", err)
	}
	if !slices.Equal(host.Calls, []string{"list"}) || len(rt.Commands) != 0 {
		t.Errorf("herdr calls = %v, VM commands = %v, want nothing touched", host.Calls, rt.Commands)
	}
}

func TestDisableForStopDisablesTheMachineThenStopsAndShowsHowToEnableIt(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{Machines: []herdr.Machine{{ID: "m1", Target: rt.SSHTarget("app"), Enabled: true}}}
	var progress strings.Builder
	var stoppedWhileEnabled []bool

	err := herdr.Machines{Client: host, VM: rt}.DisableForStop(context.Background(), "app", func(context.Context) error {
		stoppedWhileEnabled = append(stoppedWhileEnabled, host.Machines[0].Enabled)
		return nil
	}, &progress)

	if err != nil || !slices.Equal(stoppedWhileEnabled, []bool{false}) {
		t.Errorf("DisableForStop() = %v, stopped while enabled = %v, want one stop after disabling", err, stoppedWhileEnabled)
	}
	if !strings.Contains(progress.String(), "herdr machine enable m1") {
		t.Errorf("progress = %q, want how to enable the machine again", progress.String())
	}
}

func TestDisableForStopEnablesTheMachineAgainWhenTheStopFails(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{Machines: []herdr.Machine{{ID: "m1", Target: rt.SSHTarget("app"), Enabled: true}}}
	failed := errors.New("sbx stop: exit status 1")

	err := herdr.Machines{Client: host, VM: rt}.DisableForStop(context.Background(), "app", func(context.Context) error { return failed }, io.Discard)

	if !errors.Is(err, failed) || !host.Machines[0].Enabled {
		t.Errorf("DisableForStop() = %v, machine = %+v, want the stop failure and the machine enabled again", err, host.Machines[0])
	}
}

func TestDisableForStopDoesNotStopWhenTheMachineCannotBeDisabled(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{Machines: []herdr.Machine{{ID: "m1", Target: rt.SSHTarget("app"), Enabled: true}}, FailDisable: true}
	stopped := false

	err := herdr.Machines{Client: host, VM: rt}.DisableForStop(context.Background(), "app", func(context.Context) error {
		stopped = true
		return nil
	}, io.Discard)

	if err == nil || stopped {
		t.Errorf("DisableForStop() = %v, stopped = %v, want an error without stopping", err, stopped)
	}
}

func TestRemoveRemovesTheMachineOfTheSandbox(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{Machines: []herdr.Machine{{ID: "m1", Target: rt.SSHTarget("app")}, {ID: "m2", Target: rt.SSHTarget("other")}}}

	err := herdr.Machines{Client: host, VM: rt}.Remove(context.Background(), "app")

	if err != nil || len(host.Machines) != 1 || host.Machines[0].ID != "m2" {
		t.Errorf("Remove() = %v, machines = %v, want only the app machine removed", err, host.Machines)
	}
}

func TestRemoveWithoutARegistrationDoesNothing(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{}

	err := herdr.Machines{Client: host, VM: rt}.Remove(context.Background(), "app")

	if err != nil || !slices.Equal(host.Calls, []string{"list"}) {
		t.Errorf("Remove() = %v, herdr calls = %v, want nothing removed", err, host.Calls)
	}
}

func TestRequireOnHostShowsHowToGoOnWithoutHerdr(t *testing.T) {
	err := herdr.Machines{Client: &herdrtest.Fake{Missing: true}, VM: runningApp()}.RequireOnHost()

	if err == nil || !strings.Contains(err.Error(), "herdr.enabled: false") {
		t.Errorf("RequireOnHost() = %v, want how to go on without herdr", err)
	}
}
