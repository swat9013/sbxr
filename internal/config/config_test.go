package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testDefault = `
version: 1
profile:
  model: opus
  language: ja
  effortLevel: medium
  env:
    FIXED: "1"
  enabledPlugins:
    base-plugin@mk: true
init: []
git:
  name: base-user
  email: base@example.com
egress:
  github:
    rationale: GitHub
    allow: [github.com:443]
secret_defs:
  github:
    env: GH_TOKEN
secrets: [github]
`

// mergeYAML は各スコープの宣言を parse して merge する。空文字のスコープは宣言ファイルが無い扱いにする。
func mergeYAML(t *testing.T, defaultYAML, userYAML, repoYAML string) (Config, error) {
	t.Helper()
	var decls [3]Declaration
	for i, src := range []struct {
		scope Scope
		yaml  string
	}{{ScopeDefault, defaultYAML}, {ScopeUser, userYAML}, {ScopeRepo, repoYAML}} {
		if src.yaml == "" {
			continue
		}
		decl, err := Parse(src.scope, src.scope.String(), []byte(src.yaml))
		if err != nil {
			return Config{}, err
		}
		decls[i] = decl
	}
	return Merge(decls[0], decls[1], decls[2])
}

func mustMerge(t *testing.T, defaultYAML, userYAML, repoYAML string) Config {
	t.Helper()
	cfg, err := mergeYAML(t, defaultYAML, userYAML, repoYAML)
	if err != nil {
		t.Fatalf("merge error = %v", err)
	}
	return cfg
}

func assertErrorMentions(t *testing.T, err error, needle string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want an error mentioning %q", needle)
	}
	if !strings.Contains(err.Error(), needle) {
		t.Errorf("error = %q, want it to mention %q", err, needle)
	}
}

// --- スコープ制限表: 1 行につき 1 test ---

func TestRepoCannotOverrideBaseFixedProfileKeys(t *testing.T) {
	for key, value := range map[string]string{
		"language":                "en",
		"outputStyle":             "Explanatory",
		"feedbackDrafts":          "notify",
		"awaySummaryEnabled":      "true",
		"autoMemoryEnabled":       "true",
		"autoDreamEnabled":        "true",
		"promptSuggestionEnabled": "true",
		"spinnerTipsEnabled":      "true",
		"env":                     "{X: '1'}",
		"extraKnownMarketplaces":  "{m: {source: {source: github, repo: a/b}}}",
	} {
		t.Run(key, func(t *testing.T) {
			_, err := mergeYAML(t, testDefault, "", "version: 1\nprofile:\n  "+key+": "+value+"\n")
			assertErrorMentions(t, err, "profile."+key)
		})
	}
}

func TestRepoCannotDeclareSecretDefs(t *testing.T) {
	_, err := mergeYAML(t, testDefault, "", "version: 1\nsecret_defs:\n  evil:\n    host: evil.example.com\n")
	assertErrorMentions(t, err, "secret_defs")
}

func TestRepoEgressBecomesSandboxScopeOnly(t *testing.T) {
	cfg := mustMerge(t, testDefault, "", "version: 1\negress:\n  npm:\n    rationale: npm\n    allow: [registry.npmjs.org:443]\n")

	if _, ok := cfg.SandboxEgress["npm"]; !ok {
		t.Errorf("SandboxEgress = %v, want it to contain the repo group npm", cfg.SandboxEgress)
	}
	if _, ok := cfg.GlobalEgress["npm"]; ok {
		t.Errorf("GlobalEgress = %v, want the repo group npm to stay out of global rules", cfg.GlobalEgress)
	}
}

func TestDefaultAndUserEgressBecomeGlobalRules(t *testing.T) {
	cfg := mustMerge(t, testDefault, "version: 1\negress:\n  pypi:\n    rationale: PyPI\n    allow: [pypi.org:443]\n", "")

	want := map[string]EgressGroup{
		"github": {Rationale: "GitHub", Allow: []string{"github.com:443"}},
		"pypi":   {Rationale: "PyPI", Allow: []string{"pypi.org:443"}},
	}
	if !reflect.DeepEqual(cfg.GlobalEgress, want) {
		t.Errorf("GlobalEgress = %v, want %v", cfg.GlobalEgress, want)
	}
	if len(cfg.SandboxEgress) != 0 {
		t.Errorf("SandboxEgress = %v, want empty", cfg.SandboxEgress)
	}
}

func TestUserIsTrustedToDeclareBaseFixedKeysAndSecretDefs(t *testing.T) {
	cfg := mustMerge(t, testDefault, `
version: 1
profile:
  language: en
  awaySummaryEnabled: true
secret_defs:
  gitlab:
    env: GITLAB_TOKEN
`, "")

	if got := *cfg.Profile.Language; got != "en" {
		t.Errorf("Profile.Language = %q, want %q", got, "en")
	}
	if got := *cfg.Profile.AwaySummaryEnabled; got != true {
		t.Errorf("Profile.AwaySummaryEnabled = %v, want true", got)
	}
	if _, ok := cfg.SecretDefs["gitlab"]; !ok {
		t.Errorf("SecretDefs = %v, want it to contain the user definition gitlab", cfg.SecretDefs)
	}
	if _, ok := cfg.SecretDefs["github"]; !ok {
		t.Errorf("SecretDefs = %v, want it to keep the default definition github", cfg.SecretDefs)
	}
}

func TestSecretsAreUnionedAcrossScopes(t *testing.T) {
	cfg := mustMerge(t, testDefault, "version: 1\nsecrets: [gitlab, github]\n", "version: 1\nsecrets: [atlassian, gitlab]\n")

	want := []string{"github", "gitlab", "atlassian"}
	if !reflect.DeepEqual(cfg.Secrets, want) {
		t.Errorf("Secrets = %v, want %v", cfg.Secrets, want)
	}
}

// --- merge 規則: map 系は additive、scalar は override ---

func TestMergeRules(t *testing.T) {
	tests := []struct {
		name     string
		userYAML string
		repoYAML string
		got      func(Config) any
		want     any
	}{
		{
			name: "宣言ファイルが無ければ default だけが結果になる",
			got:  func(c Config) any { return []any{*c.Profile.Model, c.Git, c.Init} },
			want: []any{"opus", Identity{Name: "base-user", Email: "base@example.com"}, []string(nil)},
		},
		{
			name:     "model と effortLevel は repo が override できる",
			repoYAML: "version: 1\nprofile:\n  model: sonnet\n  effortLevel: high\n",
			got:      func(c Config) any { return []string{*c.Profile.Model, *c.Profile.EffortLevel, *c.Profile.Language} },
			want:     []string{"sonnet", "high", "ja"},
		},
		{
			name:     "enabledPlugins は repo が additive に足せる",
			repoYAML: "version: 1\nprofile:\n  enabledPlugins:\n    repo-plugin@mk: true\n",
			got:      func(c Config) any { return c.Profile.EnabledPlugins },
			want:     map[string]bool{"base-plugin@mk": true, "repo-plugin@mk": true},
		},
		{
			name:     "enabledPlugins は user も additive に足せる",
			userYAML: "version: 1\nprofile:\n  enabledPlugins:\n    user-plugin@mk: true\n",
			got:      func(c Config) any { return c.Profile.EnabledPlugins },
			want:     map[string]bool{"base-plugin@mk": true, "user-plugin@mk": true},
		},
		{
			name:     "env は user が additive に足せる",
			userYAML: "version: 1\nprofile:\n  env:\n    USER_VAR: '1'\n",
			got:      func(c Config) any { return c.Profile.Env },
			want:     map[string]string{"FIXED": "1", "USER_VAR": "1"},
		},
		{
			name:     "user は override 可の scalar を上書きできる",
			userYAML: "version: 1\nprofile:\n  model: sonnet\n",
			got:      func(c Config) any { return *c.Profile.Model },
			want:     "sonnet",
		},
		{
			name:     "git は key ごとに部分 merge される",
			userYAML: "version: 1\ngit:\n  name: user-name\n",
			got:      func(c Config) any { return c.Git },
			want:     Identity{Name: "user-name", Email: "base@example.com"},
		},
		{
			name:     "repo は user の git identity も override できる",
			userYAML: "version: 1\ngit:\n  name: user-name\n  email: user@example.com\n",
			repoYAML: "version: 1\ngit:\n  name: work-user\n  email: work@example.co.jp\n",
			got:      func(c Config) any { return c.Git },
			want:     Identity{Name: "work-user", Email: "work@example.co.jp"},
		},
		{
			name:     "init と boot は default・user・repo の順に並ぶ",
			userYAML: "version: 1\ninit: [user-init]\nboot: [user-boot]\n",
			repoYAML: "version: 1\ninit: [repo-init]\nboot: [repo-boot]\n",
			got:      func(c Config) any { return [][]string{c.Init, c.Boot} },
			want:     [][]string{{"user-init", "repo-init"}, {"user-boot", "repo-boot"}},
		},
		{
			name:     "同じ名前の egress group は user が差し替える",
			userYAML: "version: 1\negress:\n  github:\n    rationale: GitHub Enterprise\n    allow: [ghe.example.com:443]\n",
			got:      func(c Config) any { return c.GlobalEgress["github"] },
			want:     EgressGroup{Rationale: "GitHub Enterprise", Allow: []string{"ghe.example.com:443"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := mustMerge(t, testDefault, tt.userYAML, tt.repoYAML)

			if got := tt.got(cfg); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

// --- 検証: 仕様外の宣言と不正値は error で止める ---

func TestInvalidDeclarationsAreRejected(t *testing.T) {
	tests := []struct {
		name     string
		userYAML string
		repoYAML string
		needle   string
	}{
		{name: "未知の top-level key", repoYAML: "version: 1\ndeny: [x.example.com]\n", needle: "deny"},
		{name: "未知の profile key", userYAML: "version: 1\nprofile:\n  modell: sonnet\n", needle: "modell"},
		{name: "未知の egress group の key", repoYAML: "version: 1\negress:\n  npm:\n    hosts: [a.example.com]\n", needle: "hosts"},
		{name: "version が無い", repoYAML: "profile:\n  model: sonnet\n", needle: "version"},
		{name: "未対応の version", userYAML: "version: 2\n", needle: "version"},
		{name: "空のファイル", repoYAML: "\n", needle: "version"},
		{name: "enabledPlugins の false で base の plugin を消す", repoYAML: "version: 1\nprofile:\n  enabledPlugins:\n    base-plugin@mk: false\n", needle: "base-plugin@mk"},
		{name: "user も enabledPlugins に false は書けない", userYAML: "version: 1\nprofile:\n  enabledPlugins:\n    p@mk: false\n", needle: "p@mk"},
		{name: "profile の scalar が空", userYAML: "version: 1\nprofile:\n  effortLevel: ''\n", needle: "profile.effortLevel"},
		{name: "profile の scalar が list", repoYAML: "version: 1\nprofile:\n  model: [opus]\n", needle: "repo"},
		{name: "bool key に文字列", userYAML: "version: 1\nprofile:\n  awaySummaryEnabled: 'false'\n", needle: "user"},
		{name: "enabledPlugins が list", repoYAML: "version: 1\nprofile:\n  enabledPlugins: [repo-plugin@mk]\n", needle: "repo"},
		{name: "boot が文字列の list でない", repoYAML: "version: 1\nboot: pgrep cron\n", needle: "repo"},
		{name: "git の値が空", repoYAML: "version: 1\ngit:\n  name: ''\n", needle: "git.name"},
		{name: "secrets の名前が空", userYAML: "version: 1\nsecrets: ['']\n", needle: "secrets"},
		{name: "secret_defs の定義が mapping でない", userYAML: "version: 1\nsecret_defs:\n  gitlab: GITLAB_TOKEN\n", needle: "user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mergeYAML(t, testDefault, tt.userYAML, tt.repoYAML)

			assertErrorMentions(t, err, tt.needle)
		})
	}
}

func TestMissingGitIdentityInEveryScopeIsAnError(t *testing.T) {
	_, err := mergeYAML(t, "version: 1\nprofile:\n  model: opus\n", "", "")

	assertErrorMentions(t, err, "git.")
}

// --- 読み込み ---

func TestLoadReadsUserAndRepoFilesOnTopOfTheEmbeddedDefault(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "config.yaml")
	repoPath := filepath.Join(dir, "sbxr.yaml")
	writeFile(t, userPath, "version: 1\ngit:\n  name: probe-user\n  email: probe@example.com\n")
	writeFile(t, repoPath, "version: 1\nprofile:\n  model: sonnet\n")

	cfg, err := Load(userPath, repoPath)

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Git != (Identity{Name: "probe-user", Email: "probe@example.com"}) || *cfg.Profile.Model != "sonnet" {
		t.Errorf("Load() = git %v model %q, want user identity and repo model", cfg.Git, *cfg.Profile.Model)
	}
}

func TestLoadTreatsMissingFilesAsAbsentScopes(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "config.yaml")
	writeFile(t, userPath, "version: 1\ngit:\n  name: probe-user\n  email: probe@example.com\n")

	_, err := Load(userPath, filepath.Join(dir, "no-such-sbxr.yaml"))

	if err != nil {
		t.Errorf("Load() error = %v, want a missing repo declaration to be treated as absent", err)
	}
}

func TestLoadNamesTheFileThatFailedToParse(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "config.yaml")
	writeFile(t, userPath, "version: 1\nallow: [x.example.com]\n")

	_, err := Load(userPath, filepath.Join(dir, "sbxr.yaml"))

	assertErrorMentions(t, err, userPath)
}

// --- 同梱の default スコープ ---

func TestEmbeddedDefaultIsGenericAndCarriesNoPersonalValues(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "config.yaml")
	writeFile(t, userPath, "version: 1\ngit:\n  name: probe-user\n  email: probe@example.com\n")

	cfg, err := Load(userPath, filepath.Join(dir, "sbxr.yaml"))

	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Profile.Language != nil {
		t.Errorf("Profile.Language = %q, want the default scope to leave language to the user", *cfg.Profile.Language)
	}
	if len(cfg.Profile.EnabledPlugins) != 0 {
		t.Errorf("Profile.EnabledPlugins = %v, want the default scope to leave plugin choice to the user", cfg.Profile.EnabledPlugins)
	}
	if cfg.Git != (Identity{Name: "probe-user", Email: "probe@example.com"}) {
		t.Errorf("Git = %v, want the identity to come from the user scope only", cfg.Git)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
