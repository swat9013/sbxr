package sandbox

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

func runningVM(t *testing.T, fake *sbxstub.FakeVM) vm {
	t.Helper()
	stub := &sbxstub.Stub{Sandboxes: map[string]string{"app": "running"}, VM: fake}
	v, err := openVM(context.Background(), runtime.NewSbx(stub.Run), "app")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestInitWarnsAndStillRunsWhenAptDoesNotFinishInTime(t *testing.T) {
	saved := aptWait
	t.Cleanup(func() { aptWait = saved })
	aptWait.budget, aptWait.interval, aptWait.sleep = 3*time.Second, time.Second, func(time.Duration) {}
	fake := &sbxstub.FakeVM{AptBusyPolls: 100}
	var progress bytes.Buffer

	err := runInit(context.Background(), runningVM(t, fake), "/repo", []string{"make"}, &progress)

	if err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	if fake.AptPolls != 3 || !strings.Contains(progress.String(), "警告") {
		t.Errorf("apt polls = %d, progress = %q, want 3 polls and a warning", fake.AptPolls, progress.String())
	}
	if len(fake.ShellRuns) != 1 {
		t.Errorf("shell runs = %v, want init to run after the warning", fake.ShellRuns)
	}
}

func TestBootScriptRunsEachCommandInTheRepoRootEvenWithQuotes(t *testing.T) {
	repo := t.TempDir()
	marker := filepath.Join(repo, "out")
	script := bootScript(repo, []string{"echo \"it's\" >> out", "false", "echo second >> out"})

	out, err := exec.Command("bash", "-c", script).CombinedOutput()

	if err == nil {
		t.Errorf("boot script exited 0, want the failed entry reported (output %q)", out)
	}
	if !strings.Contains(string(out), "boot[2] fail") {
		t.Errorf("output = %q, want the failed entry named", out)
	}
	data, _ := exec.Command("cat", marker).Output()
	if string(data) != "it's\nsecond\n" {
		t.Errorf("marker = %q, want both commands run in the repo root", data)
	}
}
