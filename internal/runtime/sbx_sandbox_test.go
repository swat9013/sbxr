package runtime

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/swat9013/sbxr/internal/assets"
	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

func TestStartupOutcomeReadsOnlyTheLatestDispatcherRun(t *testing.T) {
	failedThenComplete := "=== dispatcher run 1 ===\nfail /etc/durable-startup.d/002-startup-sbxr-herdr/000-cmd.sh exit=1\n" +
		"=== dispatcher run 2 ===\nok /etc/durable-startup.d/002-startup-sbxr-herdr/000-cmd.sh\n=== dispatcher complete ===\n"
	completeThenRunning := "=== dispatcher run 1 ===\n=== dispatcher complete ===\n=== dispatcher run 2 ===\n> /etc/durable-startup.d/002-startup-sbxr-herdr/000-cmd.sh\n"
	for _, tc := range []struct {
		name, log string
		want      startup
	}{
		{"no log yet", "", startupRunning},
		{"a failure in the latest run", "=== dispatcher run 1 ===\nfail /etc/durable-startup.d/003-startup-sbxr-boot/000-cmd.sh exit=1\n", startupFailed},
		{"command output that starts with fail", "=== dispatcher run 1 ===\nfail to resolve host, retrying\n=== dispatcher complete ===\n", startupComplete},
		{"an earlier failure is not the latest run", failedThenComplete, startupComplete},
		{"an earlier completion is not the latest run", completeThenRunning, startupRunning},
	} {
		if got := startupOutcome(tc.log); got != tc.want {
			t.Errorf("%s: startupOutcome = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestTheHerdrKitReportsEachFailureWithTheLineSbxrReads(t *testing.T) {
	spec, err := fs.ReadFile(assets.Kits(), herdrKit+"/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}

	for _, stage := range []string{"install", "worktree", "config", "server", "integration"} {
		if !strings.Contains(string(spec), `echo "`+herdrKitFailure+stage) {
			t.Errorf("herdr kit has no %q line for the %s stage", herdrKitFailure+stage, stage)
		}
	}
}

func TestWaitingForKitStartupGivesUpAfterTheBudget(t *testing.T) {
	saved := startupWait
	t.Cleanup(func() { startupWait = saved })
	startupWait.Budget, startupWait.Interval, startupWait.Sleep = 3*time.Second, time.Second, func(time.Duration) {}
	fake := &sbxstub.FakeVM{Files: map[string]string{kitStartupLog: "=== dispatcher run ===\n> /etc/durable-startup.d/002-startup-sbxr-herdr/000-cmd.sh\n"}}
	stub := &sbxstub.Stub{Sandboxes: map[string]string{"app": "running"}, VM: fake}

	err := NewSbx(stub.Run).waitStartup(context.Background(), "app")

	if err == nil || !strings.Contains(err.Error(), "終わらない") {
		t.Errorf("waitStartup() error = %v, want a timeout", err)
	}
}

func TestTheBootKitRunsTheScriptWhereSbxrWritesIt(t *testing.T) {
	spec, err := fs.ReadFile(assets.Kits(), bootKit+"/spec.yaml")

	if err != nil || !strings.Contains(string(spec), `"$HOME/`+BootScriptRelPath+`"`) {
		t.Errorf("sbxr-boot spec = %q, %v, want it to run $HOME/%s", spec, err, BootScriptRelPath)
	}
}

func TestAFailedSecretIsNamedWithTheSecretsAlreadyPlaced(t *testing.T) {
	stub := &sbxstub.Stub{FailOn: "secret set-custom"}
	sbx := NewSbx(stub.Run)
	dir := t.TempDir()
	spec := SandboxSpec{Name: "app", Repo: "/src/app", Secrets: []SandboxSecret{
		{Service: "github", Value: "v"},
		{Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Value: "v"},
	}}
	if err := sbx.DefineSandbox(dir, spec); err != nil {
		t.Fatal(err)
	}

	err := sbx.CreateSandbox(context.Background(), dir, spec)

	if err == nil || !strings.Contains(err.Error(), "secret env GITLAB_TOKEN を置けない") || !strings.Contains(err.Error(), "置いた分: [service github]") {
		t.Errorf("CreateSandbox() error = %v, want the failed secret and the placed ones", err)
	}
}

func TestTheEnvDefinitionCarriesTheHerdrKitWithItsVersionAndTheWorktreeOnlyWhenHerdrIsInstalled(t *testing.T) {
	for name, install := range map[string]*HerdrInstall{"installed": {Version: "v0.9.0"}, "not installed": nil} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()

			if err := NewSbx((&sbxstub.Stub{}).Run).DefineSandbox(dir, SandboxSpec{Name: "app", Repo: "/src/app", Herdr: install}); err != nil {
				t.Fatal(err)
			}

			env, _, err := readEnvDefinition(dir)
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(env.Kits, func(kit envKit) bool { return kit.Source == "./kits/"+herdrKit })
			if (install == nil) != (i < 0) || (install != nil && (env.Kits[i].Args["version"] != install.Version || env.Kits[i].Args["worktree"] != "/src/app")) {
				t.Errorf("env kits = %+v, want the herdr kit only when installed, with its version and the worktree", env.Kits)
			}
			if _, err := fs.Stat(os.DirFS(dir), kitsDir+"/"+herdrKit+"/spec.yaml"); (install == nil) != (err != nil) {
				t.Errorf("herdr kit in the state dir: %v, want it only when installed", err)
			}
		})
	}
}

// herdrWorktreeOK が拒む文字は、kit が作業ツリーの path を bash の単一引用符の中へ差し込むことから決まる。
// kit が別の形で差し込み始めたら、検査も見直す。
func TestTheHerdrKitCarriesTheWorktreeOnlyInsideSingleQuotes(t *testing.T) {
	spec, err := fs.ReadFile(assets.Kits(), herdrKit+"/spec.yaml")
	if err != nil {
		t.Fatal(err)
	}

	all, quoted := strings.Count(string(spec), "${{ kit.args.worktree }}"), strings.Count(string(spec), "'${{ kit.args.worktree }}'")

	if all == 0 || all != quoted {
		t.Errorf("herdr kit embeds the worktree %d times, %d of them in single quotes, want every one in single quotes", all, quoted)
	}
}

// herdr の kit は VM 内の作業ツリーの path を bash の単一引用符の中へ差し込む。
func TestDefineSandboxRefusesAWorktreeThatTheHerdrKitCannotCarry(t *testing.T) {
	for _, repo := range []string{"/src/it's", "/src/a\nb", "/src/caf\xe9"} {
		dir := t.TempDir()

		err := NewSbx((&sbxstub.Stub{}).Run).DefineSandbox(dir, SandboxSpec{Name: "app", Repo: repo, Herdr: &HerdrInstall{Version: "v0.9.0"}})

		if _, found, _ := readEnvDefinition(dir); err == nil || found {
			t.Errorf("DefineSandbox(%q) = %v, definition written = %v, want it refused before writing", repo, err, found)
		}
	}
}

func TestDefineSandboxAcceptsAnyWorktreeWithoutHerdr(t *testing.T) {
	if err := NewSbx((&sbxstub.Stub{}).Run).DefineSandbox(t.TempDir(), SandboxSpec{Name: "app", Repo: "/src/it's"}); err != nil {
		t.Errorf("DefineSandbox() = %v, want no check of the worktree without herdr", err)
	}
}

func TestCreateSandboxSetsTheSecretsBeforeCreatingAndAddsTheRulesAfter(t *testing.T) {
	stub := &sbxstub.Stub{VM: &sbxstub.FakeVM{}}
	sbx := NewSbx(stub.Run)
	dir := t.TempDir()
	spec := SandboxSpec{Name: "app", Repo: "/src/app", Secrets: []SandboxSecret{{Service: "github", Value: "v"}}, EgressRules: []string{"api.example.com:443"}}
	if err := sbx.DefineSandbox(dir, spec); err != nil {
		t.Fatal(err)
	}

	if err := sbx.CreateSandbox(context.Background(), dir, spec); err != nil {
		t.Fatal(err)
	}

	secretAt := slices.Index(stub.Writes, "secret set github --sandbox app")
	createAt := slices.IndexFunc(stub.Writes, func(w string) bool { return strings.HasPrefix(w, "env create") })
	ruleAt := slices.IndexFunc(stub.Writes, func(w string) bool { return strings.HasPrefix(w, "policy allow network --sandbox app") })
	if secretAt < 0 || createAt < 0 || ruleAt < 0 || secretAt > createAt || createAt > ruleAt {
		t.Errorf("sbx writes = %q, want the secret before env create and the rule after it", stub.Writes)
	}
}

func TestCreateSandboxReportsAFailureAfterTheVMWasCreatedWithItsStep(t *testing.T) {
	for _, tc := range []struct {
		name    string
		arrange func(*sbxstub.Stub)
		spec    SandboxSpec
		want    CreatedStep
	}{
		{"a sandbox rule", func(s *sbxstub.Stub) { s.FailOn = "policy allow network --sandbox" },
			SandboxSpec{Name: "app", Repo: "/src/app", EgressRules: []string{"api.example.com:443"}}, CreatedStepSandboxEgress},
		{"the herdr kit", func(s *sbxstub.Stub) { s.VM.FailHerdrKit() },
			SandboxSpec{Name: "app", Repo: "/src/app", Herdr: &HerdrInstall{Version: "v0.9.0"}}, CreatedStepHerdrStartup},
		{"the kit dispatcher", func(s *sbxstub.Stub) { s.VM.FailKitStartup() },
			SandboxSpec{Name: "app", Repo: "/src/app", Herdr: &HerdrInstall{Version: "v0.9.0"}}, CreatedStepHerdrStartup},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &sbxstub.Stub{VM: &sbxstub.FakeVM{}}
			tc.arrange(stub)
			sbx := NewSbx(stub.Run)
			dir := t.TempDir()
			if err := sbx.DefineSandbox(dir, tc.spec); err != nil {
				t.Fatal(err)
			}

			err := sbx.CreateSandbox(context.Background(), dir, tc.spec)

			var created *CreatedError
			if !errors.As(err, &created) || created.Step != tc.want {
				t.Errorf("CreateSandbox() error = %v, want a failure after the VM was created at step %d", err, tc.want)
			}
			if stub.Sandboxes["app"] != "running" {
				t.Errorf("sandboxes = %v, want the VM kept running", stub.Sandboxes)
			}
		})
	}
}

func TestTheEnvDefinitionCarriesTheBootKitOnlyWhenBootIsReplayed(t *testing.T) {
	for name, replay := range map[string]bool{"replayed": true, "not replayed": false} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()

			if err := NewSbx((&sbxstub.Stub{}).Run).DefineSandbox(dir, SandboxSpec{Name: "app", Repo: "/src/app", ReplayBoot: replay}); err != nil {
				t.Fatal(err)
			}

			env, _, err := readEnvDefinition(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := env.carriesKit(bootKit); got != replay {
				t.Errorf("env kits = %+v, want the boot kit %v", env.Kits, replay)
			}
		})
	}
}
