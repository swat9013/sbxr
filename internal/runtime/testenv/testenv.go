// Package testenv は test 用の実行基盤 (sbx stub と in-memory の Runtime adapter) が env 定義を読み書きする。
// sbx の env 定義のうち、VM の名前 (name) だけを扱う。
package testenv

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

const file = "sbxenv.yaml"

// Name は dir の env 定義が指す sandbox VM の名前を読む。env 定義が無ければ error (sbx の実測)。
func Name(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return "", fmt.Errorf("no sbxenv.yaml found at %s: %w", dir, err)
	}
	var env struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil || env.Name == "" {
		return "", fmt.Errorf("%s/sbxenv.yaml の name を読めない", dir)
	}
	return env.Name, nil
}

// Write は dir に name の sandbox VM を指す env 定義を書く。
func Write(dir, name string) error {
	return os.WriteFile(filepath.Join(dir, file), []byte("name: "+name+"\n"), 0o600)
}
