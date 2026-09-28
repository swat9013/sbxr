package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/herdr"
)

// v0.1.0 が作った状態ディレクトリを、新しい sbxr が扱えることを固定する (ADR 0006 の改訂)。
// fixture の中身と作り方は testdata/v0.1.0/README.md。

const v010URL = "https://example.com/me/app.git"

// v010Lifecycle は v0.1.0 が作った状態ディレクトリ fixture (testdata/v0.1.0 の下) の sandbox VM app が status でいて、
// その herdr machine id1 が登録されている状態で動かす。
func v010Lifecycle(t *testing.T, fixture, status string) *lifecycle {
	t.Helper()
	lc := herdrLifecycle(t)
	lc.clonedRepoDecl = repoWithEgress
	if err := os.CopyFS(lc.stateDir("app"), os.DirFS("testdata/v0.1.0/"+fixture)); err != nil {
		t.Fatal(err)
	}
	lc.stub.Sandboxes = map[string]string{"app": status}
	lc.herdr.Machines = []herdr.Machine{{ID: "id1", Target: lc.deps.runtime.SSHTarget("app"), Enabled: true}}
	return lc
}

func TestCreateOfAV010SandboxReportsItAsExistingInsteadOfCreating(t *testing.T) {
	lc := v010Lifecycle(t, "app", "running")

	out, err := lc.run(t, "create", v010URL, "--yes")

	if err != nil {
		t.Errorf("create error = %v, want the existing VM reported without drift", err)
	}
	if !strings.Contains(out, "sandbox VM app は既にある") {
		t.Errorf("output = %q, want the existing VM reported", out)
	}
	if slices.ContainsFunc(lc.stub.Writes, func(w string) bool { return strings.HasPrefix(w, "env create") }) {
		t.Errorf("sbx writes = %q, want nothing created", lc.stub.Writes)
	}
}

func TestPlanOfAV010SandboxComparesWithItsRecord(t *testing.T) {
	lc := v010Lifecycle(t, "app", "running")

	out := lc.mustRun(t, "plan", v010URL)

	if !strings.Contains(out, "drift: 作成時の宣言との差分は無い") {
		t.Errorf("output = %q, want no drift from the v0.1.0 record read with its dropped repo egress", out)
	}
}

func TestStopOfAV010SandboxDisablesItsHerdrMachine(t *testing.T) {
	lc := v010Lifecycle(t, "app", "running")

	lc.mustRun(t, "stop", v010URL)

	if !slices.Contains(lc.herdr.Calls, "disable id1") || lc.stub.Sandboxes["app"] != "stopped" {
		t.Errorf("herdr calls = %v, status = %q, want the machine disabled and the VM stopped", lc.herdr.Calls, lc.stub.Sandboxes["app"])
	}
}

func TestDestroyOfAV010SandboxRemovesTheVMItsHerdrMachineAndTheStateDir(t *testing.T) {
	lc := v010Lifecycle(t, "app", "stopped")

	lc.mustRun(t, "destroy", v010URL, "--yes")

	if _, ok := lc.stub.Sandboxes["app"]; ok || !slices.Contains(lc.herdr.Calls, "remove id1") || exists(lc.stateDir("app")) {
		t.Errorf("sandboxes = %v, herdr calls = %v, want the VM, the machine and the state dir removed", lc.stub.Sandboxes, lc.herdr.Calls)
	}
}

func TestDestroyOfAHalfCreatedV010SandboxRemovesItsHerdrMachineByItsEnvDefinition(t *testing.T) {
	lc := v010Lifecycle(t, "app-half-created", "running")

	lc.mustRun(t, "destroy", v010URL, "--yes", "--force")

	if !slices.Contains(lc.herdr.Calls, "remove id1") {
		t.Errorf("herdr calls = %v, want the machine removed", lc.herdr.Calls)
	}
	if exists(lc.stateDir("app")) {
		t.Errorf("the state dir was kept, want it removed")
	}
}
