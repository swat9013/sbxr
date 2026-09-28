package sandbox

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/inmemory"
)

// runningVM は稼働中の VM app を持つ in-memory の実行基盤を返す。respond は VM 内のコマンドへの応答で、
// printenv HOME には /home/agent を返す。
func runningVM(t *testing.T, respond func(runtime.SandboxCommand) ([]byte, error)) (vm, *inmemory.Runtime) {
	t.Helper()
	rt := inmemory.New()
	rt.Sandbox("app").Status = runtime.SandboxRunning
	rt.Respond = func(_ string, command runtime.SandboxCommand) ([]byte, error) {
		if slices.Equal(command.Args, []string{"printenv", "HOME"}) {
			return []byte("/home/agent\n"), nil
		}
		if respond == nil {
			return nil, nil
		}
		return respond(command)
	}
	v, err := openVM(context.Background(), rt, "app")
	if err != nil {
		t.Fatal(err)
	}
	return v, rt
}

// shellRuns は VM 内で bash -c で走ったコマンド。
func shellRuns(rt *inmemory.Runtime) []string {
	var runs []string
	for _, command := range rt.Commands {
		if len(command.Args) == 3 && command.Args[0] == "bash" && command.Args[1] == "-c" {
			runs = append(runs, command.Args[2])
		}
	}
	return runs
}

func TestInitWarnsAndStillRunsWhenAptDoesNotFinishInTime(t *testing.T) {
	saved := aptWait
	t.Cleanup(func() { aptWait = saved })
	aptWait.budget, aptWait.interval, aptWait.sleep = 3*time.Second, time.Second, func(time.Duration) {}
	aptRunning := func(command runtime.SandboxCommand) ([]byte, error) {
		if slices.Equal(command.Args, []string{"pgrep", "-x", "apt-get"}) {
			return []byte("123\n"), nil
		}
		return nil, nil
	}
	v, rt := runningVM(t, aptRunning)
	var progress bytes.Buffer

	err := runInit(context.Background(), v, "/repo", []string{"make"}, &progress)

	if err != nil {
		t.Fatalf("runInit() error = %v", err)
	}
	if !strings.Contains(progress.String(), "警告") {
		t.Errorf("progress = %q, want a warning", progress.String())
	}
	if runs := shellRuns(rt); !slices.Equal(runs, []string{"make"}) {
		t.Errorf("shell runs = %v, want init to run after the warning", runs)
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
