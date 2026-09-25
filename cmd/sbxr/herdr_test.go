package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/herdr"
)

// fakeHerdr は host の herdr を再現する。呼ばれた操作を Calls に起きた順に並べる。
type fakeHerdr struct {
	Machines []herdr.Machine
	Calls    []string
	// Missing が true なら host に herdr が無い。
	Missing                          bool
	FailAdd, FailDisable, FailRemove bool
}

var _ herdr.Client = (*fakeHerdr)(nil)

func (f *fakeHerdr) Available() error {
	f.Calls = append(f.Calls, "available")
	if f.Missing {
		return errors.New("host に herdr が無い")
	}
	return nil
}

func (f *fakeHerdr) List(context.Context) ([]herdr.Machine, error) {
	f.Calls = append(f.Calls, "list")
	return slices.Clone(f.Machines), nil
}

func (f *fakeHerdr) Add(_ context.Context, target, label string) error {
	f.Calls = append(f.Calls, "add "+target+" "+label)
	if f.FailAdd {
		return errors.New("herdr machine add: exit status 1")
	}
	f.Machines = append(f.Machines, herdr.Machine{ID: fmt.Sprintf("id%d", len(f.Machines)+1), Label: label, Target: target, Enabled: true})
	return nil
}

func (f *fakeHerdr) Disable(_ context.Context, id string) error {
	f.Calls = append(f.Calls, "disable "+id)
	if f.FailDisable {
		return errors.New("herdr machine disable: exit status 1")
	}
	for i := range f.Machines {
		if f.Machines[i].ID == id {
			f.Machines[i].Enabled = false
		}
	}
	return nil
}

func (f *fakeHerdr) Remove(_ context.Context, id string) error {
	f.Calls = append(f.Calls, "remove "+id)
	if f.FailRemove {
		return errors.New("herdr machine remove: exit status 1")
	}
	f.Machines = slices.DeleteFunc(f.Machines, func(m herdr.Machine) bool { return m.ID == id })
	return nil
}

const herdrUserConfig = lifecycleUserConfig + "herdr:\n  enabled: true\n"

const kitStartupLogPath = "/var/log/sbx-kit-startup.log"

// herdrLifecycle は herdr 連携を有効にした user 設定と、kit startup を終えた VM で動かす。
func herdrLifecycle(t *testing.T) *lifecycle {
	t.Helper()
	lc := newLifecycle(t, herdrUserConfig)
	lc.stub.VM.Files = map[string]string{kitStartupLogPath: "=== dispatcher run ===\nok /etc/durable-startup.d/001-startup-sbxr-herdr/000-cmd.sh\n=== dispatcher complete ===\n"}
	return lc
}

// --- herdr 連携が無効 ---

func TestHerdrIsNeverCalledWhenDisabled(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")

	lc.mustRun(t, "create", repo, "--yes")
	lc.mustRun(t, "stop", repo)
	lc.mustRun(t, "destroy", repo, "--yes")

	if len(lc.herdr.Calls) != 0 {
		t.Errorf("herdr calls = %v, want none", lc.herdr.Calls)
	}
	if slices.Contains(lc.stub.VM.Events, "pkill herdr") {
		t.Errorf("VM events = %v, want the herdr server left alone", lc.stub.VM.Events)
	}
}

func TestTheEnvDefinitionLeavesOutTheHerdrKitWhenDisabled(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "")

	lc.mustRun(t, "create", repo, "--yes")

	stateDir := lc.places.StateDir("app")
	env, _ := os.ReadFile(filepath.Join(stateDir, "sbxenv.yaml"))
	if strings.Contains(string(env), "sbxr-herdr") || exists(filepath.Join(stateDir, "kits", "sbxr-herdr")) {
		t.Errorf("sbxenv.yaml = %q, want no herdr kit", env)
	}
}

// --- herdr 連携が有効 ---

func TestTheEnvDefinitionCarriesTheHerdrKitWithThePinnedVersion(t *testing.T) {
	lc := herdrLifecycle(t)
	repo := localRepo(t, "app", "")

	lc.mustRun(t, "create", repo, "--yes")

	stateDir := lc.places.StateDir("app")
	env, _ := os.ReadFile(filepath.Join(stateDir, "sbxenv.yaml"))
	if !strings.Contains(string(env), "./kits/sbxr-herdr") || !strings.Contains(string(env), "version: v0.9.0") {
		t.Errorf("sbxenv.yaml = %q, want the herdr kit with the default version", env)
	}
	if !exists(filepath.Join(stateDir, "kits", "sbxr-herdr", "spec.yaml")) {
		t.Errorf("herdr kit was not written to the state dir")
	}
}

func TestCreateRegistersTheSandboxAsAHerdrMachineAfterStoppingTheKitsServer(t *testing.T) {
	lc := herdrLifecycle(t)
	repo := localRepo(t, "app", "")

	lc.mustRun(t, "create", repo, "--yes")

	if !slices.Contains(lc.herdr.Calls, "add app.sbx app") {
		t.Errorf("herdr calls = %v, want app.sbx registered with label app", lc.herdr.Calls)
	}
	if !slices.Contains(lc.stub.VM.Events, "pkill herdr") {
		t.Errorf("VM events = %v, want the kit's server stopped before the registration", lc.stub.VM.Events)
	}
}

func TestCreateDoesNotRegisterAMachineThatIsAlreadyRegistered(t *testing.T) {
	lc := herdrLifecycle(t)
	lc.herdr.Machines = []herdr.Machine{{ID: "old", Label: "app", Target: "app.sbx", Enabled: true}}
	repo := localRepo(t, "app", "")

	lc.mustRun(t, "create", repo, "--yes")

	if slices.ContainsFunc(lc.herdr.Calls, func(c string) bool { return strings.HasPrefix(c, "add ") }) {
		t.Errorf("herdr calls = %v, want no second registration", lc.herdr.Calls)
	}
}

func TestStopDisablesTheHerdrMachineFirstAndShowsHowToEnableIt(t *testing.T) {
	lc := herdrLifecycle(t)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")

	out := lc.mustRun(t, "stop", repo)

	if !slices.Contains(lc.herdr.Calls, "disable id1") {
		t.Errorf("herdr calls = %v, want the machine disabled", lc.herdr.Calls)
	}
	if lc.stub.Sandboxes["app"] != "stopped" {
		t.Errorf("status = %q, want stopped", lc.stub.Sandboxes["app"])
	}
	if !strings.Contains(out, "herdr machine enable id1") {
		t.Errorf("output = %q, want how to enable the machine again", out)
	}
}

func TestStopKeepsTheVMRunningWhenTheMachineCannotBeDisabled(t *testing.T) {
	lc := herdrLifecycle(t)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	lc.herdr.FailDisable = true

	_, err := lc.run(t, "stop", repo)

	if err == nil {
		t.Fatal("stop succeeded, want the disable failure")
	}
	if lc.stub.Sandboxes["app"] != "running" {
		t.Errorf("status = %q, want the VM left running (herdr would restart a stopped VM)", lc.stub.Sandboxes["app"])
	}
}

func TestDestroyRemovesTheHerdrMachine(t *testing.T) {
	lc := herdrLifecycle(t)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	lc.mustRun(t, "stop", repo)

	lc.mustRun(t, "destroy", repo, "--yes")

	if !slices.Contains(lc.herdr.Calls, "remove id1") || len(lc.herdr.Machines) != 0 {
		t.Errorf("herdr calls = %v, machines = %v, want the machine removed", lc.herdr.Calls, lc.herdr.Machines)
	}
}

func TestDestroyContinuesPastAHerdrRemovalFailureAndShowsHowToRemoveIt(t *testing.T) {
	lc := herdrLifecycle(t)
	repo := localRepo(t, "app", "")
	lc.mustRun(t, "create", repo, "--yes")
	lc.mustRun(t, "stop", repo)
	lc.herdr.FailRemove = true

	out, err := lc.run(t, "destroy", repo, "--yes")

	if err == nil || !strings.Contains(out, "herdr machine remove id1") {
		t.Errorf("error = %v, output = %q, want a non-zero exit with the removal command", err, out)
	}
	if _, ok := lc.stub.Sandboxes["app"]; ok {
		t.Errorf("sandbox still exists, want destroy to continue past the herdr failure")
	}
	if exists(lc.places.StateDir("app")) {
		t.Errorf("state dir remains, want destroy to continue past the herdr failure")
	}
}

func TestCreateKeepsTheVMAndShowsHowToRegisterWhenTheRegistrationFails(t *testing.T) {
	lc := herdrLifecycle(t)
	lc.herdr.FailAdd = true
	repo := localRepo(t, "app", "")

	_, err := lc.run(t, "create", repo, "--yes")

	if err == nil || !strings.Contains(err.Error(), "herdr machine add app.sbx --label app") {
		t.Errorf("error = %v, want a non-zero exit with the registration command", err)
	}
	if lc.stub.Sandboxes["app"] != "running" {
		t.Errorf("status = %q, want the VM kept", lc.stub.Sandboxes["app"])
	}
	if !exists(filepath.Join(lc.places.StateDir("app"), "declaration.yaml")) {
		t.Errorf("declaration.yaml missing, want the creation itself to count as finished")
	}
}

func TestCreateStopsBeforeTheGateWhenTheHostHasNoHerdr(t *testing.T) {
	lc := herdrLifecycle(t)
	lc.herdr.Missing = true
	repo := localRepo(t, "app", "")

	_, err := lc.run(t, "create", repo, "--yes")

	if err == nil {
		t.Fatal("create succeeded, want the missing herdr reported")
	}
	if _, ok := lc.stub.Sandboxes["app"]; ok || exists(lc.places.StateDir("app")) {
		t.Errorf("sandbox or state dir was created, want nothing created")
	}
}

func TestCreateFailsAndKeepsTheVMWhenTheKitStartupFails(t *testing.T) {
	lc := herdrLifecycle(t)
	lc.stub.VM.Files[kitStartupLogPath] = "=== dispatcher run ===\nfail /etc/durable-startup.d/001-startup-sbxr-herdr/000-cmd.sh\n"
	repo := localRepo(t, "app", "")

	_, err := lc.run(t, "create", repo, "--yes")

	if err == nil || !strings.Contains(err.Error(), kitStartupLogPath) {
		t.Errorf("error = %v, want the kit startup log pointed at", err)
	}
	if slices.ContainsFunc(lc.herdr.Calls, func(c string) bool { return strings.HasPrefix(c, "add ") }) {
		t.Errorf("herdr calls = %v, want no registration", lc.herdr.Calls)
	}
}
