package sbxstub

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeEnvWithKit は env 定義と、それが ./kits/<name> で指す kit の spec.yaml を dir に書く。
func writeEnvWithKit(t *testing.T, name, spec string) string {
	t.Helper()
	dir := t.TempDir()
	env := "schemaVersion: \"1\"\nname: app\nkits:\n  - source: ./kits/" + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, envFile), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "kits", name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kits", name, "spec.yaml"), []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestEnvCreateRefusesKitsThatSbxRefuses(t *testing.T) {
	for _, tc := range []struct {
		name, spec, want string
	}{
		{"a v2 kit mixed with the v3 claude", "schemaVersion: \"2\"\nkind: mixin\n",
			"cannot mix v3 kits with v1/v2 kits in one sandbox; compositions are single-format (v3: claude; v1/v2: probe)"},
		{"a v1 kit mixed with the v3 claude", "schemaVersion: \"1\"\n",
			"cannot mix v3 kits with v1/v2 kits in one sandbox; compositions are single-format (v3: claude; v1/v2: probe)"},
		{"a v3 kit without the syntax line", "schemaVersion: \"3\"\nkind: mixin\n", "kit probe の build が失敗した"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := &Stub{}
			dir := writeEnvWithKit(t, "probe", tc.spec)

			_, err := stub.Run(context.Background(), nil, "env", "create", "--force", dir)

			if err == nil || !strings.Contains(err.Error(), tc.want) || len(stub.Sandboxes) != 0 {
				t.Errorf("env create = %v, sandboxes = %v, want %q and no sandbox", err, stub.Sandboxes, tc.want)
			}
		})
	}
}

// 期待値は sbx v0.53.0 の実測の組 (docs/design/sbxr/system.md の 2026-11-19 の実測の #152) をそのまま写す。
func TestTheVMRunsKitStartupsInTheOrderOfTheirSourcePaths(t *testing.T) {
	dir := t.TempDir()
	sources := []string{"./p1/probe-y", "./p0/probe-z", "./p3/aaa-probe", "./p2/probe-x"}
	env := "schemaVersion: \"1\"\nname: app\nkits:\n"
	for _, source := range sources {
		env += "  - source: " + source + "\n"
		if err := os.MkdirAll(filepath.Join(dir, source), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, source, "spec.yaml"), []byte(kitSyntax+"\nschemaVersion: \"3\"\nkind: mixin\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, envFile), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}
	stub := &Stub{}

	if _, err := stub.Run(context.Background(), nil, "env", "create", "--force", dir); err != nil {
		t.Fatal(err)
	}

	if got, want := stub.KitStartupOrders["app"], []string{"probe-z", "probe-y", "probe-x", "aaa-probe"}; !slices.Equal(got, want) {
		t.Errorf("kit startup order = %q, want %q", got, want)
	}
}

func TestEnvCreateAcceptsAV3KitWithTheSyntaxLine(t *testing.T) {
	stub := &Stub{}
	dir := writeEnvWithKit(t, "fine", kitSyntax+"\nschemaVersion: \"3\"\nkind: mixin\n")

	if _, err := stub.Run(context.Background(), nil, "env", "create", "--force", dir); err != nil || stub.Sandboxes["app"] != "running" {
		t.Errorf("env create = %v, sandboxes = %v, want app running", err, stub.Sandboxes)
	}
}
