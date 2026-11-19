package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// launch は launchScript を host の bash で走らせる (VM の中の bash と同じく、script の path と log の path を引数で渡す)。
func launch(t *testing.T, script, log string) (string, error) {
	t.Helper()
	out, err := exec.Command("bash", "-c", launchScript, "bash", script, log).CombinedOutput()
	return string(out), err
}

func TestLaunchingAScriptThatCannotRunFailsInsteadOfReportingItStarted(t *testing.T) {
	dir := t.TempDir()
	notExecutable := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(notExecutable, []byte("#!/bin/bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, script := range map[string]string{"無い script": filepath.Join(dir, "missing.sh"), "実行できない script": notExecutable} {
		t.Run(name, func(t *testing.T) {
			if out, err := launch(t, script, filepath.Join(dir, "state", "run.log")); err == nil {
				t.Errorf("launch = nil (output %q), want a non-zero exit", out)
			}
		})
	}
}

func TestLaunchingFailsWhenTheLogCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(dir, "state")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil { // log のディレクトリの置き場がファイルで塞がっている
		t.Fatal(err)
	}

	if out, err := launch(t, script, filepath.Join(blocker, "run.log")); err == nil || !strings.Contains(out, "を書けない") {
		t.Errorf("launch = %v (output %q), want it to fail saying the log cannot be written", err, out)
	}
}

func TestLaunchingRunsTheScriptInTheBackgroundAndAppendsItsOutputToTheLog(t *testing.T) {
	if _, err := exec.LookPath("setsid"); err != nil {
		t.Skip("host に setsid が無い (VM の中には有る前提。実 sbx の確認に残す)")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\necho started\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "state", "run.log")

	if out, err := launch(t, script, log); err != nil {
		t.Fatalf("launch = %v (output %q)", err, out)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		data, _ := os.ReadFile(log)
		if strings.Contains(string(data), "started") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("log = %q, want the script output appended", data)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
