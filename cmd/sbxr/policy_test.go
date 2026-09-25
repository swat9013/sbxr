package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	content := "version: 1\negress:\n"
	for _, name := range []string{"mise-tools", "docker-registry", "ubuntu-apt", "cert-validation"} {
		content += "  " + name + ":\n    enabled: false\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPolicySyncCheckReportsTheDiffAndFailsWithoutWriting(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{sbxstub.GlobalAllow("r1", "evil.example.com:443")}}
	deps := dependencies{runtime: runtime.NewSbx(stub.Run), userConfigPath: onlyGithubUserConfig(t)}

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

func TestPolicySyncConvergesSoThatCheckPassesAfterwards(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{sbxstub.GlobalAllow("r1", "evil.example.com:443")}}
	deps := dependencies{runtime: runtime.NewSbx(stub.Run), userConfigPath: onlyGithubUserConfig(t)}

	if _, err := runSbxr(t, deps, "policy", "sync"); err != nil {
		t.Fatalf("policy sync error = %v", err)
	}
	out, err := runSbxr(t, deps, "policy", "sync", "--check")

	if err != nil {
		t.Errorf("policy sync --check after sync error = %v, output = %q", err, out)
	}
}
