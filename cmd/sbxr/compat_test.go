package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/herdr"
)

// v0.1.0 が作った状態ディレクトリを、新しい sbxr が扱えることを固定する (ADR 0006 の改訂)。
// testdata/v0.1.0/app は v0.1.0 の sbxr が git URL の repo から herdr 連携を有効にし、--yes で作った状態ディレクトリ
// (repo の egress を落とした印を含む)。v0.1.0 の test harness で生成し、env 定義の workspace の path だけを固定値に置き換えた。

const v010URL = "https://example.com/me/app.git"

// v010Lifecycle は v0.1.0 が作った sandbox VM app が status でいて、その herdr machine id1 が登録されている状態で動かす。
func v010Lifecycle(t *testing.T, status string) *lifecycle {
	t.Helper()
	lc := herdrLifecycle(t)
	lc.clonedRepoDecl = repoWithEgress
	if err := os.CopyFS(lc.places.StateDir("app"), os.DirFS("testdata/v0.1.0/app")); err != nil {
		t.Fatal(err)
	}
	lc.stub.Sandboxes = map[string]string{"app": status}
	lc.herdr.Machines = []herdr.Machine{{ID: "id1", Target: lc.deps.runtime.SSHTarget("app"), Enabled: true}}
	return lc
}

func TestCreateOfAV010SandboxReportsItAsExistingInsteadOfCreating(t *testing.T) {
	lc := v010Lifecycle(t, "running")

	out, err := lc.run(t, "create", v010URL, "--yes")

	if !strings.Contains(out+errText(err), "既にあ") {
		t.Errorf("output = %q, error = %v, want the existing VM reported", out, err)
	}
	if slices.ContainsFunc(lc.stub.Writes, func(w string) bool { return strings.HasPrefix(w, "env create") }) {
		t.Errorf("sbx writes = %q, want nothing created", lc.stub.Writes)
	}
}

func TestPlanOfAV010SandboxComparesWithItsRecord(t *testing.T) {
	lc := v010Lifecycle(t, "running")

	out := lc.mustRun(t, "plan", v010URL)

	if !strings.Contains(out, "drift:") {
		t.Errorf("output = %q, want the drift from the v0.1.0 record", out)
	}
}

func TestStopOfAV010SandboxDisablesItsHerdrMachine(t *testing.T) {
	lc := v010Lifecycle(t, "running")

	lc.mustRun(t, "stop", v010URL)

	if !slices.Contains(lc.herdr.Calls, "disable id1") || lc.stub.Sandboxes["app"] != "stopped" {
		t.Errorf("herdr calls = %v, status = %q, want the machine disabled and the VM stopped", lc.herdr.Calls, lc.stub.Sandboxes["app"])
	}
}

func TestDestroyOfAV010SandboxRemovesTheVMItsHerdrMachineAndTheStateDir(t *testing.T) {
	lc := v010Lifecycle(t, "stopped")

	lc.mustRun(t, "destroy", v010URL, "--yes")

	if _, ok := lc.stub.Sandboxes["app"]; ok || !slices.Contains(lc.herdr.Calls, "remove id1") || exists(lc.places.StateDir("app")) {
		t.Errorf("sandboxes = %v, herdr calls = %v, want the VM, the machine and the state dir removed", lc.stub.Sandboxes, lc.herdr.Calls)
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
