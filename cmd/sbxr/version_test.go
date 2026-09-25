package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	tests := []struct {
		name          string
		linkedVersion string
		buildInfo     *debug.BuildInfo
		want          string
	}{
		{
			name:          "ldflags で埋め込んだ版があればそれを使う",
			linkedVersion: "v0.1.0",
			buildInfo:     &debug.BuildInfo{Main: debug.Module{Version: "v0.0.9"}},
			want:          "v0.1.0",
		},
		{
			name:          "ldflags が無ければ go install で記録された module の版を使う",
			linkedVersion: "",
			buildInfo:     &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}},
			want:          "v0.1.0",
		},
		{
			name:          "どちらも無ければ (devel) を返す",
			linkedVersion: "",
			buildInfo:     nil,
			want:          "(devel)",
		},
		{
			name:          "build info の版が空なら (devel) を返す",
			linkedVersion: "",
			buildInfo:     &debug.BuildInfo{},
			want:          "(devel)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveVersion(tt.linkedVersion, tt.buildInfo)
			if got != tt.want {
				t.Errorf("resolveVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}
