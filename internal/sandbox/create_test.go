package sandbox

import (
	"context"
	"errors"
	"io"
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

// preparedApp は secret を 1 つ配線し、repo の egress を 1 つ持つ sandbox VM app の作る内容。herdr・plugin・init は持たない。
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

func TestCreateHandsTheRuntimeWhatToCreate(t *testing.T) {
	prepared := preparedApp()
	rt := inmemory.New()
	rt.Respond = vmAnswers(append(slices.Clone(prepared.GlobalEgress), prepared.Declaration.SandboxEgress...))
	places := Places{StateRoot: t.TempDir()}

	err := Create(context.Background(), Hosts{Runtime: rt}, places, prepared, secret.Values{"GITLAB_TOKEN": "glpat_x"}, io.Discard)

	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	want := runtime.SandboxSpec{
		Name: "app", Repo: "/src/app",
		Env:         map[string]string{"GITLAB_HOST": "gitlab.example.com"},
		Secrets:     []runtime.SandboxSecret{{Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Value: "glpat_x"}},
		EgressRules: []string{"api.example.com:443"},
	}
	if got := rt.Sandbox("app").Spec; !reflect.DeepEqual(got, want) {
		t.Errorf("spec = %+v, want %+v", got, want)
	}
}

func TestCreateAsksTheRuntimeToInstallTheDeclaredHerdr(t *testing.T) {
	prepared := preparedApp()
	prepared.Declaration.Herdr = &HerdrPin{Version: "v0.9.0"}
	rt := inmemory.New()
	rt.Respond = vmAnswers(prepared.GlobalEgress)
	places := Places{StateRoot: t.TempDir()}

	// host の herdr が無いので登録の段で止まるが、作る内容はその前に渡っている
	_ = Create(context.Background(), Hosts{Runtime: rt, Herdr: missingHerdr{}}, places, prepared, secret.Values{"GITLAB_TOKEN": "glpat_x"}, io.Discard)

	if got := rt.Sandbox("app").Spec.Herdr; got == nil || got.Version != "v0.9.0" {
		t.Errorf("spec.Herdr = %+v, want v0.9.0", got)
	}
}

func TestCreateShowsAFailureAfterTheVMWasCreatedAsItsStage(t *testing.T) {
	err := createdStageError(&runtime.CreatedError{Step: runtime.CreatedStepSandboxEgress, Err: io.ErrUnexpectedEOF})

	var stage *StageError
	if !errors.As(err, &stage) || stage.Stage != StageSandboxEgress {
		t.Errorf("error = %v, want the sandbox egress stage", err)
	}
}

func TestCreateWritesNothingWhenAWiredValueIsMissing(t *testing.T) {
	rt := inmemory.New()
	places := Places{StateRoot: t.TempDir()}

	err := Create(context.Background(), Hosts{Runtime: rt}, places, preparedApp(), secret.Values{}, io.Discard)

	if err == nil || !strings.Contains(err.Error(), "GITLAB_TOKEN") || len(rt.Definitions) != 0 {
		t.Errorf("Create() = %v, definitions = %v, want to stop before defining the sandbox", err, rt.Definitions)
	}
}

// missingHerdr は host に herdr が無い状態。
type missingHerdr struct{}

var errNoHerdr = errors.New("herdr が PATH に無い")

func (missingHerdr) Available() error                              { return errNoHerdr }
func (missingHerdr) List(context.Context) ([]herdr.Machine, error) { return nil, errNoHerdr }
func (missingHerdr) Add(context.Context, string, string) error     { return errNoHerdr }
func (missingHerdr) Enable(context.Context, string) error          { return errNoHerdr }
func (missingHerdr) Disable(context.Context, string) error         { return errNoHerdr }
func (missingHerdr) Remove(context.Context, string) error          { return errNoHerdr }
