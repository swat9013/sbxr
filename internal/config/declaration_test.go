package config

import (
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestAProfileWritesOnlyItsDeclaredKeysIncludingFalse(t *testing.T) {
	off := false

	data, err := yaml.Marshal(Profile{AutoMemoryEnabled: &off, EnabledPlugins: map[string]bool{}})

	// 宣言していない key (nil・空の map) は書かない。false は宣言した値なので書く
	if want := "autoMemoryEnabled: false\n"; err != nil || string(data) != want {
		t.Errorf("yaml.Marshal(Profile) = %q, %v, want %q", data, err, want)
	}
}
