package sbxstub

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// envFile は sbx env create・env rm が dir から読む env 定義のファイル名 (sbx の実測)。
// stub は sbx の読み方を写すので、Sbx adapter の書き方とは独立に持つ (食い違いは Sbx の契約 test が検出する)。
const envFile = "sbxenv.yaml"

// envDefinition は stub が env 定義から読む中身。
type envDefinition struct {
	// Name は env 定義が指す sandbox VM の名前。
	Name string
	// SkillsMode は sandboxOptions.skills。無ければ空 (sbx の既定の共有)。値は確かめない (作成の側で確かめる)。
	SkillsMode string
	// dir は env 定義のディレクトリ。kitSources の ./ で始まる source はここを基準に解決する (sbx の実測)。
	dir string
	// kitSources は env 定義の kits に書かれた順の source (混在の error は kit 名をこの順に並べる)。
	kitSources []string
}

// readEnv は dir の env 定義を読む。env 定義が無いか、読めないか、name が無ければ error (sbx の実測)。
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
		Kits []struct {
			Source string `yaml:"source"`
		} `yaml:"kits"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil {
		return envDefinition{}, fmt.Errorf("sbxstub: %s/%s を読めない: %w", dir, envFile, err)
	}
	if env.Name == "" {
		return envDefinition{}, fmt.Errorf("sbxstub: %s/%s の name を読めない", dir, envFile)
	}
	def := envDefinition{Name: env.Name, SkillsMode: env.SandboxOptions.Skills, dir: dir}
	for _, kit := range env.Kits {
		def.kitSources = append(def.kitSources, kit.Source)
	}
	return def, nil
}

// kitSyntax は v3 の source kit の descriptor が 1 行目に要る行。無いと build が descriptor を Dockerfile として読む
// (sbx v0.53.0 の実測。docs/design/sbxr/system.md の 2026-11-17 の実測)。
const kitSyntax = "# syntax=docker.io/docker/sandbox-kit:3"

// checkKits は作成に使う env 定義の kit を、sbx v0.53.0 の作成と同じく確かめる。agent の claude は v3 の kit に
// 解決されるので、v1・v2 の kit が 1 つでもあれば混在として拒む。v3 の kit は 1 行目の syntax 行が無ければ build で失敗する。
// 混在の拒否は kit の解決の段で起き、build より先なので、先に返す。
func (env envDefinition) checkKits() error {
	var legacy, v3 []string
	var buildErr error
	for _, source := range env.kitSources {
		spec, err := os.ReadFile(filepath.Join(env.dir, source, "spec.yaml"))
		if err != nil {
			return fmt.Errorf("sbxstub: kit %s を読めない: %w", source, err)
		}
		var descriptor struct {
			SchemaVersion string `yaml:"schemaVersion"`
		}
		if err := yaml.Unmarshal(spec, &descriptor); err != nil {
			return fmt.Errorf("sbxstub: kit %s の spec.yaml を読めない: %w", source, err)
		}
		name := kitName(source)
		if descriptor.SchemaVersion != "3" {
			legacy = append(legacy, name)
			continue
		}
		v3 = append(v3, name)
		if first, _, _ := strings.Cut(string(spec), "\n"); first != kitSyntax && buildErr == nil {
			buildErr = fmt.Errorf("sbxstub: kit %s の build が失敗した (dockerfile parse error: unknown instruction: schemaVersion:)", name)
		}
	}
	if len(legacy) > 0 {
		return fmt.Errorf("sbxstub: resolve kits: cannot mix v3 kits with v1/v2 kits in one sandbox; compositions are single-format (v3: %s; v1/v2: %s)",
			strings.Join(append([]string{"claude"}, v3...), ", "), strings.Join(legacy, ", "))
	}
	return buildErr
}

// kitStartupOrder は env 定義の kit の名前を、VM の起動で startup が走る順に返す。sbx v0.53.0 は kits の書かれた順でも
// kit 名の順でもなく、source の path の辞書順に走らせる (docs/design/sbxr/system.md の 2026-11-19 の実測の #152)。
func (env envDefinition) kitStartupOrder() []string {
	sources := slices.Sorted(slices.Values(env.kitSources))
	names := make([]string, len(sources))
	for i, source := range sources {
		names[i] = kitName(source)
	}
	return names
}

// kitName は source が指す kit の名前。v3 の kit 名は directory 名から決まる (sbx の実測)。
func kitName(source string) string { return filepath.Base(source) }

// envName は dir の env 定義が指す sandbox VM の名前を読む。env 定義が無ければ error (sbx の実測)。
func envName(dir string) (string, error) {
	env, err := readEnv(dir)
	return env.Name, err
}

// skillsModes は作成で sandboxOptions.skills が受ける値。空は sbx の既定 (sbx create --skills の help、sbx v0.48.0)。
var skillsModes = []string{"", "off", "readonly", "readwrite"}

// checkSkillsMode は作成に使う env 定義の sandboxOptions.skills が sbx の受ける値かを確かめる。
func (env envDefinition) checkSkillsMode() error {
	if !slices.Contains(skillsModes, env.SkillsMode) {
		return fmt.Errorf("sbxstub: sandboxOptions.skills %q は off・readonly・readwrite のどれでもない", env.SkillsMode)
	}
	return nil
}
