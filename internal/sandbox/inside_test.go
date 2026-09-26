package sandbox

import (
	"bytes"
	"context"
	"io/fs"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/swat9013/sbxr/internal/assets"
	"github.com/swat9013/sbxr/internal/config"
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
	if !strings.Contains(progress.String(), "警告") {
		t.Errorf("progress = %q, want a warning", progress.String())
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

func TestMergeSettingsKeepsLowerKeysAndLetsUpperWin(t *testing.T) {
	lower := map[string]any{"permissions": map[string]any{"defaultMode": "bypass"}, "env": map[string]any{"A": "1"}, "list": []any{"x"}, "model": "opus"}
	upper := map[string]any{"env": map[string]any{"B": "2"}, "list": []any{"y"}, "model": "sonnet"}

	got := mergeSettings(lower, upper)

	want := map[string]any{"permissions": map[string]any{"defaultMode": "bypass"}, "env": map[string]any{"A": "1", "B": "2"}, "list": []any{"y"}, "model": "sonnet"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeSettings() = %v, want %v", got, want)
	}
}

func TestContainsAllowsExtraKeysInMapsButNotOtherDifferences(t *testing.T) {
	for name, tc := range map[string]struct {
		got, want any
		ok        bool
	}{
		"map に余分な key":   {map[string]any{"A": "1", "B": "2"}, map[string]any{"A": "1"}, true},
		"入れ子の map の値が違う": {map[string]any{"A": map[string]any{"x": 1.0}}, map[string]any{"A": map[string]any{"x": 2.0}}, false},
		"map が無い":        {nil, map[string]any{"A": "1"}, false},
		"scalar が一致":     {"opus", "opus", true},
		"list は一致で比べる":   {[]any{"a", "b"}, []any{"a"}, false},
	} {
		if got := contains(tc.got, tc.want); got != tc.ok {
			t.Errorf("%s: contains() = %v, want %v", name, got, tc.ok)
		}
	}
}

func TestProfileSettingsLeavesOutUndeclaredKeys(t *testing.T) {
	model := "sonnet"

	settings, err := profileSettings(config.Profile{Model: &model, EnabledPlugins: map[string]bool{}})

	if err != nil || !reflect.DeepEqual(settings, map[string]any{"model": "sonnet"}) {
		t.Errorf("profileSettings() = %v, %v, want only the declared model", settings, err)
	}
}

func TestRemoteHostReadsOnlyNetworkRemotes(t *testing.T) {
	for remote, host := range map[string]string{
		"https://GitHub.com/me/app.git":     "github.com",
		"git@gitlab.example.com:me/app.git": "gitlab.example.com",
		"ssh://git@git.example.com/me/app":  "git.example.com",
		"/Users/me/src/upstream":            "",
		"../upstream":                       "",
		"file:///srv/git/app.git":           "",
	} {
		if got := remoteHost(remote); got != host {
			t.Errorf("remoteHost(%q) = %q, want %q", remote, got, host)
		}
	}
}

func TestTheBootKitRunsTheScriptWhereSbxrWritesIt(t *testing.T) {
	spec, err := fs.ReadFile(assets.Kits(), "sbxr-boot/spec.yaml")

	if err != nil || !strings.Contains(string(spec), `"$HOME/`+BootScriptRelPath+`"`) {
		t.Errorf("sbxr-boot spec = %q, %v, want it to run $HOME/%s", spec, err, BootScriptRelPath)
	}
}
