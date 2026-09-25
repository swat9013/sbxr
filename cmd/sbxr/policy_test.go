package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

func runSbxr(t *testing.T, deps dependencies, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd("test", deps)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// onlyGithubUserConfig は同梱の group を github 以外すべて除外する user 設定を書く。
func onlyGithubUserConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	embedded, err := config.LoadGlobalEgress(path)
	if err != nil {
		t.Fatal(err)
	}
	content := "version: 1\negress:\n"
	for name := range embedded {
		if name != "github" {
			content += "  " + name + ":\n    enabled: false\n"
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPolicySyncCheckReportsTheDiffAndFailsWithoutWriting(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{sbxstub.GlobalAllow("r1", "evil.example.com:443")}}
	deps := dependencies{runtime: runtime.NewSbx(stub.Run), userConfigPath: fixedPath(onlyGithubUserConfig(t))}

	out, err := runSbxr(t, deps, "policy", "sync", "--check")

	if err == nil {
		t.Errorf("policy sync --check error = nil, want a non-zero exit when the rules differ")
	}
	for _, line := range []string{"- rm r1 (allow: evil.example.com:443)", "+ allow github.com:443"} {
		if !strings.Contains(out, line) {
			t.Errorf("output = %q, want it to contain %q", out, line)
		}
	}
	if len(stub.Writes) != 0 {
		t.Errorf("sbx writes = %q, want none under --check", stub.Writes)
	}
}

func TestPolicySyncPrintsTheChangesMadeBeforeAFailure(t *testing.T) {
	stub := &sbxstub.Stub{FailOnWrite: 2}
	userConfig := ownGroupOnlyUserConfig(t, "a.example.com:443", "b.example.com:443")
	deps := dependencies{runtime: runtime.NewSbx(stub.Run), userConfigPath: fixedPath(userConfig)}

	out, err := runSbxr(t, deps, "policy", "sync")

	if err == nil {
		t.Errorf("policy sync error = nil, want the failed write to exit non-zero")
	}
	if !strings.Contains(out, "+ allow a.example.com:443") || strings.Contains(out, "+ allow b.example.com:443") {
		t.Errorf("output = %q, want only the add made before the failure", out)
	}
}

// ownGroupOnlyUserConfig は同梱の group をすべて除外し、allow を持つ group を 1 つだけ宣言する user 設定を書く。
func ownGroupOnlyUserConfig(t *testing.T, allow ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	embedded, err := config.LoadGlobalEgress(path)
	if err != nil {
		t.Fatal(err)
	}
	content := "version: 1\negress:\n"
	for name := range embedded {
		content += "  " + name + ":\n    enabled: false\n"
	}
	content += "  own:\n    rationale: test\n    allow:\n"
	for _, resource := range allow {
		content += "      - " + resource + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPolicySyncConvergesSoThatCheckPassesAfterwards(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{sbxstub.GlobalAllow("r1", "evil.example.com:443")}}
	deps := dependencies{runtime: runtime.NewSbx(stub.Run), userConfigPath: fixedPath(onlyGithubUserConfig(t))}

	if _, err := runSbxr(t, deps, "policy", "sync"); err != nil {
		t.Fatalf("policy sync error = %v", err)
	}
	out, err := runSbxr(t, deps, "policy", "sync", "--check")

	if err != nil {
		t.Errorf("policy sync --check after sync error = %v, output = %q", err, out)
	}
}

func fixedPath(path string) func() (string, error) {
	return func() (string, error) { return path, nil }
}
