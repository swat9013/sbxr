package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name      string
		ldflags   string
		buildInfo *debug.BuildInfo
		want      string
	}{
		{
			name:      "ldflags で埋め込んだ版があればそれを使う",
			ldflags:   "v0.1.0",
			buildInfo: &debug.BuildInfo{Main: debug.Module{Version: "v0.0.9"}},
			want:      "v0.1.0",
		},
		{
			name:      "ldflags が無ければ go install で記録された module の版を使う",
			ldflags:   "",
			buildInfo: &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}},
			want:      "v0.1.0",
		},
		{
			name:      "どちらも無ければ (devel) を返す",
			ldflags:   "",
			buildInfo: nil,
			want:      "(devel)",
		},
		{
			name:      "build info の版が空なら (devel) を返す",
			ldflags:   "",
			buildInfo: &debug.BuildInfo{},
			want:      "(devel)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveVersion(tt.ldflags, tt.buildInfo)
			if got != tt.want {
				t.Errorf("resolveVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRootCommandPrintsVersion(t *testing.T) {
	cmd := newRootCmd("v1.2.3")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if !strings.Contains(out.String(), "v1.2.3") {
		t.Errorf("--version output = %q, want it to contain %q", out.String(), "v1.2.3")
	}
}
