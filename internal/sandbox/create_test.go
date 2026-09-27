package sandbox

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/herdr"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/inmemory"
	"github.com/swat9013/sbxr/internal/secret"
)

// vmAnswers は作成の段 (materialize・read-back・egress 自己検証) が VM に送るコマンドに答える。
// git config は書いた値を覚えて返し、plugin の一覧は空、probe は answerProbes と同じに答える。
func vmAnswers(allowed []string) func(string, runtime.SandboxCommand) ([]byte, error) {
	gitConfig := map[string]string{}
	probes := answerProbes(allowed)
	return func(_ string, command runtime.SandboxCommand) ([]byte, error) {
		args := command.Args
		switch {
		case slices.Equal(args, []string{"printenv", "HOME"}):
			return []byte("/home/agent\n"), nil
		case len(args) == 6 && args[0] == "git" && args[3] == "--replace-all":
			gitConfig[args[4]] = args[5]
			return nil, nil
		case len(args) == 5 && args[0] == "git" && args[3] == "--get-all":
			return []byte(gitConfig[args[4]] + "\n"), nil
		case len(args) >= 3 && args[0] == "claude" && args[1] == "plugin" && slices.Contains(args, "--json"):
			return []byte("[]"), nil
		}
		return probes(command)
	}
}

// preparedApp は secret を 1 つ配線し、repo の egress を 1 つ持つ sandbox VM app の作る内容。herdr・plugin・init・boot は持たない。
func preparedApp() Prepared {
	var p Prepared
	p.Target = Target{Name: "app", Repo: "/src/app"}
	p.Declaration.Git.Name, p.Declaration.Git.Email = "tester", "tester@example.com"
	p.Declaration.SandboxEgress = []string{"api.example.com:443"}
	p.GlobalEgress = []string{"github.com:443", "gitlab.example.com:443"}
	p.Wiring = secret.Plan{Wired: []secret.Wire{{Name: "gitlab", Definition: secret.Definition{
		Key: "GITLAB_TOKEN", Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Vars: map[string]string{"GITLAB_HOST": "gitlab.example.com"},
	}}}}
	p.VMEnv = map[string]string{"GITLAB_HOST": "gitlab.example.com"}
	p.Declaration.Secrets = p.Wiring.WiredSecrets()
	return p
}

// newAppRuntime は preparedApp の作成の段に答える in-memory の実行基盤。
func newAppRuntime(prepared Prepared) *inmemory.Runtime {
	rt := inmemory.New()
	rt.Respond = vmAnswers(append(slices.Clone(prepared.GlobalEgress), prepared.Declaration.SandboxEgress...))
	return rt
}

func TestCreateDefinesTheSandboxWithItsNameRepoAndEnv(t *testing.T) {
	prepared := preparedApp()
	rt := newAppRuntime(prepared)
	places := Places{StateRoot: t.TempDir()}

	err := Create(context.Background(), Hosts{Runtime: rt}, places, prepared, secret.Values{"GITLAB_TOKEN": "glpat_x"}, io.Discard)

	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got := rt.Definitions[places.StateDir("app")]
	if got.Name != "app" || got.Repo != "/src/app" || !reflect.DeepEqual(got.Env, map[string]string{"GITLAB_HOST": "gitlab.example.com"}) {
		t.Errorf("definition = %+v, want app from /src/app with GITLAB_HOST", got)
	}
}

func TestCreatePlacesTheWiredSecretValueAndTheSandboxRules(t *testing.T) {
	prepared := preparedApp()
	rt := newAppRuntime(prepared)
	places := Places{StateRoot: t.TempDir()}

	err := Create(context.Background(), Hosts{Runtime: rt}, places, prepared, secret.Values{"GITLAB_TOKEN": "glpat_x"}, io.Discard)

	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	sb := rt.Sandbox("app")
	wantSecrets := []runtime.SandboxSecret{{Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Value: "glpat_x"}}
	if !reflect.DeepEqual(sb.Secrets, wantSecrets) {
		t.Errorf("secrets = %+v, want %+v", sb.Secrets, wantSecrets)
	}
	if !slices.Equal(sb.EgressRules, []string{"api.example.com:443"}) {
		t.Errorf("sandbox rules = %q, want api.example.com:443", sb.EgressRules)
	}
}

func TestCreateAsksTheRuntimeToReplayBootEvenWithoutADeclaredBoot(t *testing.T) {
	prepared := preparedApp()
	rt := newAppRuntime(prepared)
	places := Places{StateRoot: t.TempDir()}

	err := Create(context.Background(), Hosts{Runtime: rt}, places, prepared, secret.Values{"GITLAB_TOKEN": "glpat_x"}, io.Discard)

	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !rt.Definitions[places.StateDir("app")].ReplayBoot {
		t.Errorf("ReplayBoot = false, want the replay asked for")
	}
}

func TestCreateAsksTheRuntimeToInstallTheDeclaredHerdr(t *testing.T) {
	prepared := preparedApp()
	prepared.Declaration.Herdr = &HerdrPin{Version: "v0.9.0"}
	rt := newAppRuntime(prepared)
	places := Places{StateRoot: t.TempDir()}

	host := &hostHerdr{}

	err := Create(context.Background(), Hosts{Runtime: rt, Herdr: host}, places, prepared, secret.Values{"GITLAB_TOKEN": "glpat_x"}, io.Discard)

	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got := rt.Definitions[places.StateDir("app")].Herdr; got == nil || got.Version != "v0.9.0" {
		t.Errorf("definition herdr = %+v, want v0.9.0", got)
	}
	if !slices.Equal(host.added, []string{rt.SSHTarget("app")}) {
		t.Errorf("herdr machines added = %q, want the VM registered", host.added)
	}
}

func TestCreateShowsAFailureAfterTheVMWasCreatedAsItsStage(t *testing.T) {
	for step, want := range map[runtime.CreatedStep]Stage{
		runtime.CreatedStepSandboxEgress: StageSandboxEgress,
		runtime.CreatedStepHerdrStartup:  StageHerdr,
	} {
		t.Run(string(want), func(t *testing.T) {
			rt := &failingCreate{Runtime: newAppRuntime(preparedApp()), step: step}
			places := Places{StateRoot: t.TempDir()}

			err := Create(context.Background(), Hosts{Runtime: rt}, places, preparedApp(), secret.Values{"GITLAB_TOKEN": "glpat_x"}, io.Discard)

			var stage *StageError
			if !errors.As(err, &stage) || stage.Stage != want {
				t.Errorf("error = %v, want the %s stage", err, want)
			}
		})
	}
}

func TestDestroyOfAHalfCreatedSandboxRemovesTheHerdrMachineRecordedAtTheStart(t *testing.T) {
	prepared := preparedApp()
	prepared.Declaration.Herdr = &HerdrPin{Version: "v0.9.0"}
	rt := &failingCreate{Runtime: newAppRuntime(prepared), step: runtime.CreatedStepSandboxEgress}
	places := Places{StateRoot: t.TempDir()}
	host := &hostHerdr{machines: []herdr.Machine{{ID: "m1", Target: rt.SSHTarget("app")}}}
	if err := Create(context.Background(), Hosts{Runtime: rt, Herdr: host}, places, prepared, secret.Values{"GITLAB_TOKEN": "glpat_x"}, io.Discard); err == nil {
		t.Fatal("Create() = nil, want the creation stopped halfway")
	}

	_, err := Destroy(context.Background(), Hosts{Runtime: definitionUnread{rt}, Herdr: host}, places, prepared.Target, RemoveRunning)

	if err != nil || !slices.Equal(host.removed, []string{"m1"}) {
		t.Errorf("Destroy() = %v, removed = %q, want the machine removed", err, host.removed)
	}
}

func TestCreateStopsWhenAWiredValueIsMissing(t *testing.T) {
	rt := inmemory.New()
	places := Places{StateRoot: t.TempDir()}

	err := Create(context.Background(), Hosts{Runtime: rt}, places, preparedApp(), secret.Values{}, io.Discard)

	if err == nil || !strings.Contains(err.Error(), "GITLAB_TOKEN") {
		t.Errorf("Create() error = %v, want the missing GITLAB_TOKEN", err)
	}
}

func TestCreateLeavesNoStateDirWhenAWiredValueIsMissing(t *testing.T) {
	rt := inmemory.New()
	places := Places{StateRoot: t.TempDir()}

	_ = Create(context.Background(), Hosts{Runtime: rt}, places, preparedApp(), secret.Values{}, io.Discard)

	if _, err := os.Stat(places.StateDir("app")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state dir stat = %v, want no state dir", err)
	}
}

// failingCreate は VM を作れた後の段 step で失敗する実行基盤。
type failingCreate struct {
	*inmemory.Runtime
	step runtime.CreatedStep
}

func (f *failingCreate) CreateSandbox(ctx context.Context, stateDir string, spec runtime.SandboxSpec) error {
	if err := f.Runtime.CreateSandbox(ctx, stateDir, spec); err != nil {
		return err
	}
	return &runtime.CreatedError{Step: f.step, Err: io.ErrUnexpectedEOF}
}

// hostHerdr は登録と解除を受け付ける host の herdr。machines は登録済みの machine。
type hostHerdr struct {
	machines []herdr.Machine
	added    []string
	removed  []string
}

func (h *hostHerdr) Available() error { return nil }
func (h *hostHerdr) List(context.Context) ([]herdr.Machine, error) {
	return slices.Clone(h.machines), nil
}
func (h *hostHerdr) Add(_ context.Context, target, _ string) error {
	h.added = append(h.added, target)
	return nil
}
func (h *hostHerdr) Enable(context.Context, string) error  { return nil }
func (h *hostHerdr) Disable(context.Context, string) error { return nil }
func (h *hostHerdr) Remove(_ context.Context, id string) error {
	h.removed = append(h.removed, id)
	return nil
}

// definitionUnread は実行基盤の定義から herdr 連携の有無を答えない実行基盤。
// sbxr が状態ディレクトリの記録から判定することを確かめるのに使う。
type definitionUnread struct{ runtime.Runtime }

func (definitionUnread) DefinedWithHerdr(string) (bool, error) {
	return false, errors.New("実行基盤の定義は読まない")
}
