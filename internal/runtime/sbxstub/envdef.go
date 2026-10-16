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

// envName は dir の env 定義が指す sandbox VM の名前を読む。env 定義が無ければ error (sbx の実測)。
func envName(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, envFile))
	if err != nil {
		return "", fmt.Errorf("sbxstub: no %s found at %s: %w", envFile, dir, err)
	}
	var env struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil || env.Name == "" {
		return "", fmt.Errorf("sbxstub: %s/%s の name を読めない", dir, envFile)
	}
	return env.Name, nil
}

// envSkillsMode は dir の env 定義の sandboxOptions.skills を読む。無ければ空 (sbx の既定の共有)。
// sbx が受ける値は off・readonly・readwrite で、ほかの値は error (sbx create --skills の help、sbx v0.48.0)。
func envSkillsMode(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, envFile))
	if err != nil {
		return "", fmt.Errorf("sbxstub: no %s found at %s: %w", envFile, dir, err)
	}
	var env struct {
		SandboxOptions struct {
			Skills string `yaml:"skills"`
		} `yaml:"sandboxOptions"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil {
		return "", fmt.Errorf("sbxstub: %s/%s を読めない: %w", dir, envFile, err)
	}
	switch mode := env.SandboxOptions.Skills; mode {
	case "", "off", "readonly", "readwrite":
		return mode, nil
	default:
		return "", fmt.Errorf("sbxstub: %s/%s の sandboxOptions.skills %q は off・readonly・readwrite のどれでもない", dir, envFile, mode)
	}
}
