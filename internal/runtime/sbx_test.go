package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

func TestSbxListsOnlyEditableGlobalNetworkRules(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{
		sbxstub.GlobalAllow("global-1", "github.com:443"),
		{ID: "scoped", Scope: "sandbox:vm", ResourceType: "network", Decision: "allow", Resources: []string{"api.anthropic.com:443"}},
		{ID: "fs", Scope: "global", ResourceType: "filesystem:read", Decision: "allow", Resources: []string{"**"}},
		{ID: "fixed", Scope: "global", ResourceType: "network", Decision: "allow", Resources: []string{"x.example.com:443"}, Editable: false},
		{ID: "global-deny", Scope: "global", ResourceType: "network", Decision: "deny", Resources: []string{"evil.example.com:443"}, Editable: true},
	}}

	rules, err := NewSbx(stub.Run).ListGlobalEgressRules(context.Background())

	if err != nil {
		t.Fatalf("ListGlobalEgressRules() error = %v", err)
	}
	want := []EgressRule{
		{ID: "global-1", Decision: "allow", Resources: []string{"github.com:443"}},
		{ID: "global-deny", Decision: "deny", Resources: []string{"evil.example.com:443"}},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Errorf("ListGlobalEgressRules() = %+v, want %+v", rules, want)
	}
}

func TestSbxAllowsOneResourcePerCommand(t *testing.T) {
	stub := &sbxstub.Stub{}

	err := NewSbx(stub.Run).AllowGlobalEgress(context.Background(), "github.com:443")

	if err != nil {
		t.Fatalf("AllowGlobalEgress() error = %v", err)
	}
	if want := []string{"policy allow network github.com:443"}; !reflect.DeepEqual(stub.Writes, want) {
		t.Errorf("sbx writes = %q, want %q", stub.Writes, want)
	}
}

func TestSbxRemovesARuleByID(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{sbxstub.GlobalAllow("r1", "evil.example.com:443")}}

	err := NewSbx(stub.Run).RemoveGlobalEgressRule(context.Background(), "r1")

	if err != nil {
		t.Fatalf("RemoveGlobalEgressRule() error = %v", err)
	}
	if want := []string{"policy rm network --id r1"}; !reflect.DeepEqual(stub.Writes, want) {
		t.Errorf("sbx writes = %q, want %q", stub.Writes, want)
	}
}

func TestSbxRejectsARuleWithAnUnknownDecision(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{
		{ID: "r1", Scope: "global", ResourceType: "network", Decision: "audit", Resources: []string{"x.example.com:443"}, Editable: true},
	}}

	_, err := NewSbx(stub.Run).ListGlobalEgressRules(context.Background())

	if err == nil {
		t.Errorf("ListGlobalEgressRules() error = nil, want an error for the decision audit")
	}
}

func TestSbxSetsAServiceSecretScopedToTheSandboxWithTheValueOnStdin(t *testing.T) {
	stub := &sbxstub.Stub{}

	err := NewSbx(stub.Run).SetSandboxSecret(context.Background(), "vm1", SandboxSecret{Service: "github", Value: "v"})

	if err != nil {
		t.Fatalf("SetSandboxSecret() error = %v", err)
	}
	if want := []string{"secret set github --sandbox vm1"}; !reflect.DeepEqual(stub.Writes, want) {
		t.Errorf("sbx writes = %q, want %q", stub.Writes, want)
	}
	if want := []string{"v\n"}; !reflect.DeepEqual(stub.Inputs, want) {
		t.Errorf("sbx stdin = %q, want %q", stub.Inputs, want)
	}
}

func TestSbxSetsAPlaceholderSecretForEveryHost(t *testing.T) {
	stub := &sbxstub.Stub{}

	err := NewSbx(stub.Run).SetSandboxSecret(context.Background(), "vm1",
		SandboxSecret{Hosts: []string{"a.example.com", "b.example.com"}, Env: "TOKEN", Value: "v"})

	if err != nil {
		t.Fatalf("SetSandboxSecret() error = %v", err)
	}
	want := []string{"secret set-custom --sandbox vm1 --host a.example.com --host b.example.com --env TOKEN"}
	if !reflect.DeepEqual(stub.Writes, want) {
		t.Errorf("sbx writes = %q, want %q", stub.Writes, want)
	}
}

func TestSbxReadsAnEmptyOrNullSandboxListAsAbsent(t *testing.T) {
	for _, listing := range []string{`{"sandboxes":[]}`, `{"sandboxes":null}`} {
		run := func(context.Context, io.Reader, ...string) ([]byte, error) { return []byte(listing), nil }

		status, err := NewSbx(run).SandboxStatus(context.Background(), "app")

		if err != nil || status != SandboxAbsent {
			t.Errorf("SandboxStatus(%s) = %v, %v, want absent", listing, status, err)
		}
	}
}

// runInsideLocally は sbx exec [-i] <sandbox> -- <args> の <args> を host で実行する runner。
// VM の中で走る script を、実際の sh で確かめるために使う。
func runInsideLocally(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	i := slices.Index(args, "--")
	if args[0] != "exec" || i < 0 {
		return nil, fmt.Errorf("sbx exec ではない: %q", args)
	}
	cmd := exec.CommandContext(ctx, args[i+1], args[i+2:]...)
	cmd.Stdin = stdin
	return cmd.Output()
}

func TestSbxWritesAFileMakingItsParentAndSettingTheGivenMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "boot.sh")

	err := NewSbx(runInsideLocally).WriteSandboxFile(context.Background(), "app", path, []byte("echo boot\n"), 0o755)

	data, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || string(data) != "echo boot\n" {
		t.Fatalf("WriteSandboxFile = %v, file = %q, %v, want the data written with its parent made", err, data, readErr)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestSbxWritesAFileWithoutChangingItsModeWhenNoneIsGiven(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{}"), 0o640); err != nil {
		t.Fatal(err)
	}

	err := NewSbx(runInsideLocally).WriteSandboxFile(context.Background(), "app", path, []byte(`{"model":"opus"}`), KeepMode)

	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if err != nil || string(data) != `{"model":"opus"}` || info.Mode().Perm() != 0o640 {
		t.Errorf("WriteSandboxFile = %v, file = %q (%v), want the data replaced and the mode kept", err, data, info.Mode().Perm())
	}
}

func TestSbxSaysAFileInsideTheVMExists(t *testing.T) {
	present := filepath.Join(t.TempDir(), "present")
	if err := os.WriteFile(present, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	found, err := NewSbx(runInsideLocally).SandboxFileExists(context.Background(), "app", present)

	if err != nil || !found {
		t.Errorf("SandboxFileExists = %v, %v, want true", found, err)
	}
}

func TestSbxSaysAMissingFileInsideTheVMDoesNotExist(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")

	found, err := NewSbx(runInsideLocally).SandboxFileExists(context.Background(), "app", missing)

	if err != nil || found {
		t.Errorf("SandboxFileExists = %v, %v, want false", found, err)
	}
}

func TestSbxCannotTellWhetherAFileExistsWhenExecFails(t *testing.T) {
	run := func(context.Context, io.Reader, ...string) ([]byte, error) {
		return nil, errors.New("sandbox is not running")
	}

	_, err := NewSbx(run).SandboxFileExists(context.Background(), "app", "/home/agent/x")

	if err == nil {
		t.Errorf("SandboxFileExists error = nil, want the exec failure rather than absent")
	}
}

func TestSbxReadsTheStatusOfItsSandboxes(t *testing.T) {
	for status, want := range map[string]SandboxStatus{
		"running":  SandboxRunning,
		"stopped":  SandboxStopped,
		"starting": SandboxRunning, // sbx の他の値は、使用中かもしれないので稼働中と読む (destroy は拒否する側に倒れる)
	} {
		t.Run(status, func(t *testing.T) {
			listing := `{"sandboxes":[{"name":"app","status":"` + status + `"}]}`
			run := func(context.Context, io.Reader, ...string) ([]byte, error) { return []byte(listing), nil }

			got, err := NewSbx(run).SandboxStatus(context.Background(), "app")

			if err != nil || got != want {
				t.Errorf("SandboxStatus = %v, %v, want %v", got, err, want)
			}
		})
	}
}

func TestSbxConnectsToTheSandboxOverTheSSHConfigSbxWrites(t *testing.T) {
	if got := NewSbx(nil).SSHTarget("app"); got != "app.sbx" {
		t.Errorf("SSHTarget = %q, want app.sbx", got)
	}
}
