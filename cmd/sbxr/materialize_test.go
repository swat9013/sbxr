package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

const settingsPath = sbxstub.Home + "/.claude/settings.json"

// sbxInitialSettings は sbx が VM の settings.json に置く初期値の例。
const sbxInitialSettings = `{"permissions":{"defaultMode":"bypassPermissions"},"env":{"SBX_SET":"1"}}`

// gitRepo は origin を持つ git repo を作る。
func gitRepo(t *testing.T, name, repoDecl, origin string) string {
	t.Helper()
	dir := localRepo(t, name, repoDecl)
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", origin}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func vmSettings(t *testing.T, vm *sbxstub.FakeVM) map[string]any {
	t.Helper()
	var settings map[string]any
	if err := json.Unmarshal([]byte(vm.Files[settingsPath]), &settings); err != nil {
		t.Fatalf("VM settings.json = %q: %v", vm.Files[settingsPath], err)
	}
	return settings
}

const pluginUserConfig = lifecycleUserConfig + "profile:\n  enabledPlugins:\n    tool@claude-plugins-official: true\n"

// --- materialize ---

func TestCreateMergesTheProfileIntoSettingsKeepingWhatSbxPutThere(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.stub.VM.Files = map[string]string{settingsPath: sbxInitialSettings}
	repo := gitRepo(t, "app", "version: 1\nprofile:\n  model: sonnet\n", "https://github.com/me/app.git")

	lc.mustRun(t, "create", repo, "--yes")

	settings := vmSettings(t, lc.stub.VM)
	if settings["model"] != "sonnet" {
		t.Errorf("model = %v, want the declared sonnet", settings["model"])
	}
	if permissions, _ := settings["permissions"].(map[string]any); permissions["defaultMode"] != "bypassPermissions" {
		t.Errorf("permissions = %v, want sbx's initial value kept", settings["permissions"])
	}
	env, _ := settings["env"].(map[string]any)
	if env["SBX_SET"] != "1" || env["CLAUDE_CODE_ENABLE_TODO_TOOLS"] != "1" {
		t.Errorf("env = %v, want sbx's and the declared entries merged", env)
	}
}

func TestCreateInstallsDeclaredPluginsAfterAddingTheirMarketplace(t *testing.T) {
	lc := newLifecycle(t, pluginUserConfig)
	repo := gitRepo(t, "app", "", "https://github.com/me/app.git")

	lc.mustRun(t, "create", repo, "--yes")

	if !slices.Contains(lc.stub.VM.Marketplaces, "anthropics/claude-plugins-official") {
		t.Errorf("marketplaces = %v, want the declared marketplace added", lc.stub.VM.Marketplaces)
	}
	if !slices.Contains(lc.stub.VM.Plugins, "tool@claude-plugins-official") {
		t.Errorf("plugins = %v, want the declared plugin installed", lc.stub.VM.Plugins)
	}
}

func TestCreateWritesTheGitIdentityToTheVM(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := gitRepo(t, "app", "", "https://github.com/me/app.git")

	lc.mustRun(t, "create", repo, "--yes")

	config := lc.stub.VM.GitConfig
	if !slices.Equal(config["user.name"], []string{"tester"}) || !slices.Equal(config["user.email"], []string{"tester@example.com"}) {
		t.Errorf("git config = %v, want the declared identity", config)
	}
}

func TestCreateRewritesBothSSHFormsOfTheOriginHostToHTTPS(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := gitRepo(t, "app", "", "git@gitlab.example.com:me/app.git")

	lc.mustRun(t, "create", repo, "--yes")

	got := lc.stub.VM.GitConfig["url.https://gitlab.example.com/.insteadOf"]
	if !slices.Equal(got, []string{"git@gitlab.example.com:", "ssh://git@gitlab.example.com/"}) {
		t.Errorf("insteadOf = %q, want both ssh forms", got)
	}
}

func TestCreateFailsAndKeepsTheVMWhenTheReadBackDoesNotMatch(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.stub.VM.DropWrites = true
	repo := gitRepo(t, "app", "", "https://github.com/me/app.git")

	_, err := lc.run(t, "create", repo, "--yes")

	if err == nil || !strings.Contains(err.Error(), "read-back") {
		t.Errorf("error = %v, want a read-back failure", err)
	}
	if _, ok := lc.stub.Sandboxes["app"]; !ok {
		t.Errorf("sandbox was removed; the VM is kept for inspection")
	}
}

// --- init / boot ---

func TestCreateRunsInitOnceInTheRepoRoot(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := gitRepo(t, "app", "version: 1\ninit:\n  - make setup\n", "https://github.com/me/app.git")

	lc.mustRun(t, "create", repo, "--yes")

	want := []sbxstub.ShellRun{{Dir: repo, Command: "make setup"}}
	if !slices.Equal(lc.stub.VM.ShellRuns, want) {
		t.Errorf("shell runs = %+v, want %+v", lc.stub.VM.ShellRuns, want)
	}
}

func TestCreateWaitsForTheVMsAptOnlyWhenThereIsInit(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := gitRepo(t, "app", "", "https://github.com/me/app.git")

	lc.mustRun(t, "create", repo, "--yes")

	if lc.stub.VM.AptPolls != 0 {
		t.Errorf("apt polls = %d, want none without init", lc.stub.VM.AptPolls)
	}
}

func TestCreateWaitsForTheVMsAptBeforeInit(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := gitRepo(t, "app", "version: 1\ninit:\n  - apt-get install -y jq\n", "https://github.com/me/app.git")

	lc.mustRun(t, "create", repo, "--yes")

	if lc.stub.VM.AptPolls == 0 {
		t.Errorf("apt polls = 0, want a check before init")
	}
}

func TestCreateRunsBootOnceAfterInit(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := gitRepo(t, "app", "version: 1\ninit:\n  - make setup\nboot:\n  - start-daemon\n", "https://github.com/me/app.git")

	lc.mustRun(t, "create", repo, "--yes")

	bootScript := sbxstub.Home + "/.config/sbxr/boot.sh"
	if want := []string{"shell make setup", "exec " + bootScript}; !slices.Equal(lc.stub.VM.Events, want) {
		t.Errorf("VM events = %q, want init then one boot run", lc.stub.VM.Events)
	}
	if script := lc.stub.VM.Files[bootScript]; !strings.Contains(script, "start-daemon") || !strings.Contains(script, repo) {
		t.Errorf("boot script = %q, want the boot command run in the repo root", script)
	}
	if lc.stub.VM.Modes[bootScript] != "0755" {
		t.Errorf("boot script mode = %q, want 0755", lc.stub.VM.Modes[bootScript])
	}
}

func TestCreateFailsAndKeepsTheVMWhenBootFails(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	lc.stub.VM.FailExecute = sbxstub.Home + "/.config/sbxr/boot.sh"
	repo := gitRepo(t, "app", "version: 1\nboot:\n  - start-daemon\n", "https://github.com/me/app.git")

	_, err := lc.run(t, "create", repo, "--yes")

	if err == nil || !strings.Contains(err.Error(), "boot") || !strings.Contains(err.Error(), "sbxr destroy") {
		t.Errorf("error = %v, want the failed stage and the recovery steps", err)
	}
	if _, ok := lc.stub.Sandboxes["app"]; !ok {
		t.Errorf("sandbox was removed; the VM is kept")
	}
}

func TestTheEnvDefinitionCarriesTheBootKit(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := gitRepo(t, "app", "", "https://github.com/me/app.git")

	lc.mustRun(t, "create", repo, "--yes")

	stateDir := lc.places.StateDir("app")
	env, err := os.ReadFile(filepath.Join(stateDir, "sbxenv.yaml"))
	if err != nil || !strings.Contains(string(env), "./kits/sbxr-boot") {
		t.Errorf("sbxenv.yaml = %q, %v, want the boot kit", env, err)
	}
	if !exists(filepath.Join(stateDir, "kits", "sbxr-boot", "spec.yaml")) {
		t.Errorf("boot kit was not written to the state dir")
	}
}
