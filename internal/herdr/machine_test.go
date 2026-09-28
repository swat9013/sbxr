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

// hostWithEnabledMachine は sandbox VM app の、有効な herdr machine m1 を持つ host の herdr。
func hostWithEnabledMachine(rt *inmemory.Runtime) *herdrtest.Fake {
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
	vm := &observedVM{Runtime: rt, host: host}

	_ = herdr.Registry{Client: host, VM: vm}.Register(context.Background(), "app", io.Discard)

	if len(vm.machinesAtExec) != 1 || vm.machinesAtExec[0] != 0 || !strings.Contains(strings.Join(rt.Commands[0].Args, " "), "pkill -x herdr") {
		t.Errorf("machines when the VM ran a command = %v, VM commands = %v, want the server stopped before the add", vm.machinesAtExec, rt.Commands)
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
	host := hostWithEnabledMachine(rt)
	vm := &observedVM{Runtime: rt, host: host}

	_, err := herdr.Registry{Client: host, VM: vm}.DisableAndStop(context.Background(), "app", io.Discard)

	if err != nil || len(vm.enabledAtStop) != 1 || vm.enabledAtStop[0] {
		t.Errorf("DisableAndStop() = %v, machine enabled when stopped = %v, want one stop after disabling", err, vm.enabledAtStop)
	}
}

func TestDisableAndStopReturnsHowToEnableTheMachineAgain(t *testing.T) {
	rt := runningApp()

	enable, _ := herdr.Registry{Client: hostWithEnabledMachine(rt), VM: rt}.DisableAndStop(context.Background(), "app", io.Discard)

	if enable != "herdr machine enable m1" {
		t.Errorf("enable = %q, want the command to enable m1", enable)
	}
}

func TestDisableAndStopEnablesTheMachineAgainWhenTheVMCannotBeStopped(t *testing.T) {
	rt := runningApp()
	host := hostWithEnabledMachine(rt)
	registry := herdr.Registry{Client: host, VM: failingStop{rt}}

	_, err := registry.DisableAndStop(context.Background(), "app", io.Discard)

	if !errors.Is(err, errStop) || !host.Machines[0].Enabled {
		t.Errorf("DisableAndStop() = %v, machine = %+v, want the stop failure and the machine enabled again", err, host.Machines[0])
	}
}

func TestDisableAndStopShowsHowToEnableTheMachineWhenItCannotBeEnabledAgain(t *testing.T) {
	rt := runningApp()
	host := hostWithEnabledMachine(rt)
	host.FailEnable = true

	_, err := herdr.Registry{Client: host, VM: failingStop{rt}}.DisableAndStop(context.Background(), "app", io.Discard)

	if !errors.Is(err, errStop) || !strings.Contains(err.Error(), "herdr machine enable m1") {
		t.Errorf("DisableAndStop() = %v, want the stop failure and how to enable the machine", err)
	}
}

func TestDisableAndStopEnablesTheMachineAgainEvenWhenTheStopWasCancelled(t *testing.T) {
	rt := runningApp()
	host := hostWithEnabledMachine(rt)
	ctx, cancel := context.WithCancel(context.Background())
	registry := herdr.Registry{Client: cancelAware{host}, VM: cancellingStop{rt, cancel}}

	_, _ = registry.DisableAndStop(ctx, "app", io.Discard)

	if !host.Machines[0].Enabled {
		t.Errorf("machine = %+v, want it enabled again after the cancelled stop", host.Machines[0])
	}
}

func TestDisableAndStopLeavesTheVMRunningWhenTheMachineCannotBeDisabled(t *testing.T) {
	rt := runningApp()
	host := hostWithEnabledMachine(rt)
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
	host := hostWithEnabledMachine(rt)
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

// observedVM は、VM を操作した時点の host の herdr の状態を記録する実行基盤。
type observedVM struct {
	*inmemory.Runtime
	host           *herdrtest.Fake
	machinesAtExec []int
	enabledAtStop  []bool
}

func (v *observedVM) ExecInSandbox(ctx context.Context, sandbox string, command runtime.SandboxCommand) ([]byte, error) {
	v.machinesAtExec = append(v.machinesAtExec, len(v.host.Machines))
	return v.Runtime.ExecInSandbox(ctx, sandbox, command)
}

func (v *observedVM) StopSandbox(ctx context.Context, sandbox string) error {
	v.enabledAtStop = append(v.enabledAtStop, v.host.Machines[0].Enabled)
	return v.Runtime.StopSandbox(ctx, sandbox)
}

// cancellingStop は止める途中で中断される実行基盤 (Ctrl-C の再現)。
type cancellingStop struct {
	*inmemory.Runtime
	cancel context.CancelFunc
}

func (c cancellingStop) StopSandbox(context.Context, string) error {
	c.cancel()
	return context.Canceled
}

// cancelAware は中断された ctx の操作を失敗させる host の herdr (実物の CLI は中断された ctx で子プロセスを起こせない)。
type cancelAware struct{ *herdrtest.Fake }

func (c cancelAware) Enable(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return c.Fake.Enable(ctx, id)
}

// failingStop は VM を止められない実行基盤。
type failingStop struct{ *inmemory.Runtime }

func (failingStop) StopSandbox(context.Context, string) error { return errStop }
