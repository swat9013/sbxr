package sbxstub

import (
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// envFile は sbx env create・env rm が dir から読む env 定義のファイル名 (sbx の実測)。
// stub は sbx の読み方を写すので、Sbx adapter の書き方とは独立に持つ (食い違いは Sbx の契約 test が検出する)。
const envFile = "sbxenv.yaml"

// envDefinition は stub が env 定義から読む中身。
type envDefinition struct {
	// Name は env 定義が指す sandbox VM の名前。
	Name string
	// SkillsMode は sandboxOptions.skills。無ければ空 (sbx の既定の共有)。
	SkillsMode string
}

// readEnv は dir の env 定義を読む。env 定義が無いか name が無ければ error (sbx の実測)。
// sandboxOptions.skills が受ける値は off・readonly・readwrite で、ほかの値は error (sbx create --skills の help、sbx v0.48.0)。
func readEnv(dir string) (envDefinition, error) {
	data, err := os.ReadFile(filepath.Join(dir, envFile))
	if err != nil {
		return envDefinition{}, fmt.Errorf("sbxstub: no %s found at %s: %w", envFile, dir, err)
	}
	var env struct {
		Name           string `yaml:"name"`
		SandboxOptions struct {
			Skills string `yaml:"skills"`
		} `yaml:"sandboxOptions"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil || env.Name == "" {
		return envDefinition{}, fmt.Errorf("sbxstub: %s/%s の name を読めない", dir, envFile)
	}
	switch mode := env.SandboxOptions.Skills; mode {
	case "", "off", "readonly", "readwrite":
		return envDefinition{Name: env.Name, SkillsMode: mode}, nil
	default:
		return envDefinition{}, fmt.Errorf("sbxstub: %s/%s の sandboxOptions.skills %q は off・readonly・readwrite のどれでもない", dir, envFile, mode)
	}
}

// envName は dir の env 定義が指す sandbox VM の名前を読む。env 定義が無ければ error (sbx の実測)。
func envName(dir string) (string, error) {
	env, err := readEnv(dir)
	return env.Name, err
}
