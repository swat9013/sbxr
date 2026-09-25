package main

import (
	"path/filepath"
	"testing"
)

func TestUserConfigLivesUnderDotConfigEvenWithXDGConfigHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))

	got, err := defaultUserConfigPath()

	if err != nil {
		t.Fatalf("defaultUserConfigPath() error = %v", err)
	}
	if want := filepath.Join(home, ".config", "sbxr", "config.yaml"); got != want {
		t.Errorf("defaultUserConfigPath() = %q, want %q (ADR 0004)", got, want)
	}
}
