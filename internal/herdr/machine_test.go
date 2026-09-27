package herdr_test

import (
	"context"
	"errors"
	"io"
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

// enabledMachine は sandbox VM app の、有効な herdr machine m1 を持つ host の herdr。
func enabledMachine(rt *inmemory.Runtime) *herdrtest.Fake {
	return &herdrtest.Fake{Machines: []herdr.Machine{{ID: "m1", Target: rt.SSHTarget("app"), Enabled: true}}}
}

func TestRegisterAddsTheMachineOfTheSandbox(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{}

	err := herdr.Registry{Client: host, VM: rt}.Register(context.Background(), "app", io.Discard)

	if err != nil || len(host.Machines) != 1 || host.Machines[0].Target != rt.SSHTarget("app") {
		t.Errorf("Register() = %v, machines = %v, want the machine of app", err, host.Machines)
	}
}

func TestRegisterStopsTheServerInTheVMBeforeAddingTheMachine(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{}
	var commandsAtAdd []inmemory.Command
	host.OnAdd = func() { commandsAtAdd = append(commandsAtAdd, rt.Commands...) }

	_ = herdr.Registry{Client: host, VM: rt}.Register(context.Background(), "app", io.Discard)

	if len(commandsAtAdd) != 1 || !strings.Contains(strings.Join(commandsAtAdd[0].Args, " "), "pkill -x herdr") {
		t.Errorf("VM commands before the add = %v, want the herdr server stopped", commandsAtAdd)
	}
}

func TestRegisterLeavesAStaleRegistrationAloneAndShowsHowToReplaceIt(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{Machines: []herdr.Machine{{ID: "old", Target: rt.SSHTarget("app")}}}

	err := herdr.Registry{Client: host, VM: rt}.Register(context.Background(), "app", io.Discard)

	var registration *herdr.RegistrationError
	if !errors.As(err, &registration) || !strings.HasPrefix(registration.Recovery, "herdr machine remove old; ") {
		t.Errorf("Register() = %v, want the stale registration and how to replace it", err)
	}
	if len(host.Machines) != 1 || host.Machines[0].ID != "old" || len(rt.Commands) != 0 {
		t.Errorf("machines = %v, VM commands = %v, want nothing touched", host.Machines, rt.Commands)
	}
}

func TestRegisterShowsHowToRegisterByHandWhenTheMachinesCannotBeListed(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{FailList: true}

	err := herdr.Registry{Client: host, VM: rt}.Register(context.Background(), "app", io.Discard)

	var registration *herdr.RegistrationError
	if !errors.As(err, &registration) || !strings.Contains(registration.Recovery, "herdr machine add "+rt.SSHTarget("app")) {
		t.Errorf("Register() = %v, want how to register by hand", err)
	}
}

func TestDisableAndStopStopsTheVMAfterDisablingTheMachine(t *testing.T) {
	rt := runningApp()
	host := enabledMachine(rt)

	_, err := herdr.Registry{Client: host, VM: rt}.DisableAndStop(context.Background(), "app", io.Discard)

	if err != nil || host.Machines[0].Enabled || rt.Sandbox("app").Status != runtime.SandboxStopped {
		t.Errorf("DisableAndStop() = %v, machine = %+v, status = %v, want the machine disabled and the VM stopped", err, host.Machines[0], rt.Sandbox("app").Status)
	}
}

func TestDisableAndStopReturnsHowToEnableTheMachineAgain(t *testing.T) {
	rt := runningApp()

	enable, _ := herdr.Registry{Client: enabledMachine(rt), VM: rt}.DisableAndStop(context.Background(), "app", io.Discard)

	if enable != "herdr machine enable m1" {
		t.Errorf("enable = %q, want the command to enable m1", enable)
	}
}

func TestDisableAndStopEnablesTheMachineAgainWhenTheVMCannotBeStopped(t *testing.T) {
	rt := runningApp()
	host := enabledMachine(rt)
	registry := herdr.Registry{Client: host, VM: failingStop{rt}}

	_, err := registry.DisableAndStop(context.Background(), "app", io.Discard)

	if !errors.Is(err, errStop) || !host.Machines[0].Enabled {
		t.Errorf("DisableAndStop() = %v, machine = %+v, want the stop failure and the machine enabled again", err, host.Machines[0])
	}
}

func TestDisableAndStopLeavesTheVMRunningWhenTheMachineCannotBeDisabled(t *testing.T) {
	rt := runningApp()
	host := enabledMachine(rt)
	host.FailDisable = true

	_, err := herdr.Registry{Client: host, VM: rt}.DisableAndStop(context.Background(), "app", io.Discard)

	if err == nil || rt.Sandbox("app").Status != runtime.SandboxRunning {
		t.Errorf("DisableAndStop() = %v, status = %v, want an error with the VM running", err, rt.Sandbox("app").Status)
	}
}

func TestDisableAndStopLeavesTheVMRunningWhenTheMachinesCannotBeListed(t *testing.T) {
	rt := runningApp()

	_, err := herdr.Registry{Client: &herdrtest.Fake{FailList: true}, VM: rt}.DisableAndStop(context.Background(), "app", io.Discard)

	if err == nil || rt.Sandbox("app").Status != runtime.SandboxRunning {
		t.Errorf("DisableAndStop() = %v, status = %v, want an error with the VM running", err, rt.Sandbox("app").Status)
	}
}

func TestDisableAndStopLeavesAMachineTheUserDisabledAlone(t *testing.T) {
	rt := runningApp()
	host := enabledMachine(rt)
	host.Machines[0].Enabled = false
	registry := herdr.Registry{Client: host, VM: failingStop{rt}}

	_, _ = registry.DisableAndStop(context.Background(), "app", io.Discard)

	if host.Machines[0].Enabled {
		t.Errorf("machine = %+v, want it left disabled after the stop failure", host.Machines[0])
	}
}

func TestRemoveRemovesOnlyTheMachineOfTheSandbox(t *testing.T) {
	rt := runningApp()
	host := &herdrtest.Fake{Machines: []herdr.Machine{{ID: "m1", Target: rt.SSHTarget("app")}, {ID: "m2", Target: rt.SSHTarget("other")}}}

	err := herdr.Registry{Client: host, VM: rt}.Remove(context.Background(), "app")

	if err != nil || len(host.Machines) != 1 || host.Machines[0].ID != "m2" {
		t.Errorf("Remove() = %v, machines = %v, want only the app machine removed", err, host.Machines)
	}
}

func TestRemoveWithoutARegistrationSucceeds(t *testing.T) {
	rt := runningApp()
	other := herdr.Machine{ID: "m2", Target: rt.SSHTarget("other")}
	host := &herdrtest.Fake{Machines: []herdr.Machine{other}}

	err := herdr.Registry{Client: host, VM: rt}.Remove(context.Background(), "app")

	if err != nil || len(host.Machines) != 1 {
		t.Errorf("Remove() = %v, machines = %v, want nothing removed", err, host.Machines)
	}
}

var errStop = errors.New("sbx stop: exit status 1")

// failingStop は VM を止められない実行基盤。
type failingStop struct{ *inmemory.Runtime }

func (failingStop) StopSandbox(context.Context, string) error { return errStop }
