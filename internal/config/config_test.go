package config

import (
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/assets"
	"github.com/swat9013/sbxr/internal/secret"
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
    service: github
    key: GITHUB_TOKEN
    hosts: [github.com]
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
	return merge(decls[0], decls[1], decls[2])
}

func mustMerge(t *testing.T, defaultYAML, userYAML, repoYAML string) Config {
	t.Helper()
	cfg, err := mergeYAML(t, defaultYAML, userYAML, repoYAML)
	if err != nil {
		t.Fatalf("merge error = %v", err)
	}
	return cfg
}

// identity は cfg の git identity。揃っていなければ test を止める。
func identity(t *testing.T, cfg Config) Identity {
	t.Helper()
	id, err := cfg.GitIdentity()
	if err != nil {
		t.Fatalf("GitIdentity() error = %v", err)
	}
	return id
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
	_, err := mergeYAML(t, testDefault, "", "version: 1\nsecret_defs:\n  evil:\n    key: EVIL\n    hosts: [evil.example.com]\n    env: EVIL\n")
	assertErrorMentions(t, err, "secret_defs は repo 宣言には書けない")
}

func TestRepoEgressBecomesSandboxScopeOnly(t *testing.T) {
	cfg := mustMerge(t, testDefault, "", "version: 1\negress:\n  npm:\n    rationale: npm\n    allow: [registry.npmjs.org:443]\n")

	if !slices.Equal(cfg.SandboxEgress, []string{"registry.npmjs.org:443"}) {
		t.Errorf("SandboxEgress = %v, want the repo group npm", cfg.SandboxEgress)
	}
	if slices.Contains(cfg.GlobalEgress, "registry.npmjs.org:443") {
		t.Errorf("GlobalEgress = %v, want the repo group npm to stay out of global rules", cfg.GlobalEgress)
	}
}

func TestDefaultAndUserEgressBecomeGlobalRules(t *testing.T) {
	cfg := mustMerge(t, testDefault, "version: 1\negress:\n  pypi:\n    rationale: PyPI\n    allow: [pypi.org:443]\n", "")

	want := []string{"github.com:443", "pypi.org:443"}
	if !reflect.DeepEqual(cfg.GlobalEgress, want) {
		t.Errorf("GlobalEgress = %v, want %v", cfg.GlobalEgress, want)
	}
	if len(cfg.SandboxEgress) != 0 {
		t.Errorf("SandboxEgress = %v, want empty", cfg.SandboxEgress)
	}
}

func TestUserIsTrustedToDeclareBaseFixedKeys(t *testing.T) {
	cfg := mustMerge(t, testDefault, "version: 1\nprofile:\n  language: en\n  awaySummaryEnabled: true\n", "")

	if *cfg.Profile.Language != "en" || *cfg.Profile.AwaySummaryEnabled != true {
		t.Errorf("Profile = language %q awaySummaryEnabled %v, want the user values en / true", *cfg.Profile.Language, *cfg.Profile.AwaySummaryEnabled)
	}
}

func TestUserIsTrustedToAddSecretDefsOnTopOfTheDefault(t *testing.T) {
	cfg := mustMerge(t, testDefault, "version: 1\nsecret_defs:\n  gitlab:\n    key: GITLAB_TOKEN\n    hosts: [gitlab.example.com]\n    env: GITLAB_TOKEN\n", "")

	if got := slices.Sorted(maps.Keys(cfg.SecretDefs)); !reflect.DeepEqual(got, []string{"github", "gitlab"}) {
		t.Errorf("SecretDefs names = %v, want the default github and the user gitlab", got)
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
			name: "宣言ファイルが無ければ default の scalar が残る",
			got:  func(c Config) any { return *c.Profile.Model },
			want: "opus",
		},
		{
			name: "宣言ファイルが無ければ default の git identity が残る",
			got:  func(c Config) any { return identity(t, c) },
			want: Identity{Name: "base-user", Email: "base@example.com"},
		},
		{
			name: "宣言ファイルが無ければ空の init は空のまま",
			got:  func(c Config) any { return c.Init },
			want: []string(nil),
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
			got:      func(c Config) any { return identity(t, c) },
			want:     Identity{Name: "user-name", Email: "base@example.com"},
		},
		{
			name:     "repo は user の git identity も override できる",
			userYAML: "version: 1\ngit:\n  name: user-name\n  email: user@example.com\n",
			repoYAML: "version: 1\ngit:\n  name: work-user\n  email: work@example.co.jp\n",
			got:      func(c Config) any { return identity(t, c) },
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
			name:     "同じ名前の egress group は user が書いた field だけを上書きする (除外の enabled だけを重ねる)",
			userYAML: "version: 1\negress:\n  github:\n    enabled: false\n",
			got:      func(c Config) any { return c.GlobalEgress },
			want:     []string(nil),
		},
		{
			name:     "同じ名前の egress group の allow は和集合になる",
			userYAML: "version: 1\negress:\n  github:\n    allow: [ghe.example.com:443, github.com:443]\n",
			got:      func(c Config) any { return c.GlobalEgress },
			want:     []string{"ghe.example.com:443", "github.com:443"},
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
		{name: "version が無い", repoYAML: "profile:\n  model: sonnet\n", needle: "version"},
		{name: "未対応の version", userYAML: "version: 2\n", needle: "version"},
		{name: "空のファイル", repoYAML: "\n", needle: "version"},
		{name: "enabledPlugins の false で base の plugin を消す", repoYAML: "version: 1\nprofile:\n  enabledPlugins:\n    base-plugin@mk: false\n", needle: "base-plugin@mk"},
		{name: "user も enabledPlugins に false は書けない", userYAML: "version: 1\nprofile:\n  enabledPlugins:\n    p@mk: false\n", needle: "p@mk"},
		{name: "profile の scalar が空", userYAML: "version: 1\nprofile:\n  effortLevel: ''\n", needle: "profile.effortLevel"},
		{name: "profile の scalar が list", repoYAML: "version: 1\nprofile:\n  model: [opus]\n", needle: "cannot unmarshal"},
		{name: "bool key に文字列", userYAML: "version: 1\nprofile:\n  awaySummaryEnabled: 'false'\n", needle: "cannot unmarshal"},
		{name: "enabledPlugins が list", repoYAML: "version: 1\nprofile:\n  enabledPlugins: [repo-plugin@mk]\n", needle: "cannot unmarshal"},
		{name: "boot が文字列の list でない", repoYAML: "version: 1\nboot: pgrep cron\n", needle: "cannot unmarshal"},
		{name: "git の値が空", repoYAML: "version: 1\ngit:\n  name: ''\n", needle: "git.name"},
		{name: "secrets の名前が空", userYAML: "version: 1\nsecrets: ['']\n", needle: "secrets"},
		{name: "init のコマンドが空", repoYAML: "version: 1\ninit: ['']\n", needle: "init"},
		{name: "profile の scalar の値の書き忘れ", userYAML: "version: 1\nprofile:\n  effortLevel:\n", needle: "profile.effortLevel"},
		{name: "repo が base 固定 key を値なしで書く", repoYAML: "version: 1\nprofile:\n  language:\n", needle: "profile.language"},
		{name: "git の値の書き忘れ", repoYAML: "version: 1\ngit:\n  name:\n", needle: "git.name"},
		{name: "profile の scalar が mapping", repoYAML: "version: 1\nprofile:\n  effortLevel: {x: 1}\n", needle: "cannot unmarshal"},
		{name: "enabledPlugins の値が文字列", userYAML: "version: 1\nprofile:\n  enabledPlugins:\n    p@mk: 'true'\n", needle: "cannot unmarshal"},
		{name: "bool key に数値", userYAML: "version: 1\nprofile:\n  spinnerTipsEnabled: 1\n", needle: "cannot unmarshal"},
		{name: "信頼済みの user でも top-level の値の書き忘れは止める", userYAML: "version: 1\negress:\n", needle: "egress に値が無い"},
		{name: "2 つ目の YAML document", repoYAML: "version: 1\n---\ninit: [make setup]\n", needle: "document"},
		{name: "enabledPlugins の値の書き忘れ", userYAML: "version: 1\nprofile:\n  enabledPlugins:\n    p@mk:\n", needle: "p@mk"},
		{name: "secret_defs の定義が mapping でない", userYAML: "version: 1\nsecret_defs:\n  gitlab: GITLAB_TOKEN\n", needle: "cannot unmarshal"},
		{name: "egress の group に未知の field", userYAML: "version: 1\negress:\n  api:\n    rationale: API\n    allow: [api.example.com:443]\n    hosts: [x.example.com]\n", needle: "field hosts not found"},
		{name: "egress の enabled が bool でない", userYAML: "version: 1\negress:\n  github:\n    enabled: maybe\n", needle: "cannot unmarshal"},
		{name: "secret 定義に未知の field", userYAML: "version: 1\nsecret_defs:\n  api:\n    key: A\n    hosts: [a.example.com]\n    env: A\n    value: leak\n", needle: "field value not found"},
		{name: "secret 定義が不完全", userYAML: "version: 1\nsecret_defs:\n  api:\n    key: A\n    env: A\n", needle: "secret_defs.api: hosts が空"},
		{name: "herdr の値の書き忘れ", userYAML: "version: 1\nherdr:\n  enabled:\n", needle: "herdr.enabled に値が無い"},
		{name: "herdr の値が空", userYAML: "version: 1\nherdr:\n  version: ''\n", needle: "herdr.version が空"},
		{name: "egress の group の値の書き忘れ", userYAML: "version: 1\negress:\n  api:\n    rationale:\n    allow: [api.example.com:443]\n", needle: "egress.api.rationale に値が無い"},
		{name: "egress の group の値が空", userYAML: "version: 1\negress:\n  api:\n    rationale: ''\n    allow: [api.example.com:443]\n", needle: "egress.api.rationale が空"},
		{name: "alias で空文字を書く", userYAML: "version: 1\nprofile:\n  env:\n    X: &e ''\ngit:\n  email: *e\n", needle: "git.email が空"},
		{name: "alias で null を書く", userYAML: "version: 1\nprofile:\n  env:\n    X: &n ~\ngit:\n  email: *n\n", needle: "git.email に値が無い"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mergeYAML(t, testDefault, tt.userYAML, tt.repoYAML)

			assertErrorMentions(t, err, tt.needle)
		})
	}
}

func TestUserMayLeaveAnEnvValueOfTheProfileEmpty(t *testing.T) {
	if _, err := Parse(ScopeUser, "user", []byte("version: 1\nprofile:\n  env:\n    EMPTY: ''\n")); err != nil {
		t.Errorf("Parse() error = %v, want an empty env value kept as written", err)
	}
}

func TestMergeCarriesEveryProfileFieldFromTheUserScope(t *testing.T) {
	var profile Profile
	v := reflect.ValueOf(&profile).Elem()
	for _, field := range v.Fields() {
		switch field.Kind() {
		case reflect.Pointer:
			field.Set(reflect.New(field.Type().Elem()))
		case reflect.Map:
			field.Set(reflect.MakeMap(field.Type()))
		}
	}
	identity := "probe"
	user := Declaration{Profile: profile, Git: GitDeclaration{Name: &identity, Email: &identity}}

	cfg, err := merge(Declaration{}, user, Declaration{})

	if err != nil {
		t.Fatalf("merge() error = %v", err)
	}
	if !reflect.DeepEqual(cfg.Profile, profile) {
		t.Errorf("merge().Profile = %+v, want every field of %+v", cfg.Profile, profile)
	}
}

func TestMissingGitIdentityInEveryScopeIsAnError(t *testing.T) {
	cfg := mustMerge(t, "version: 1\nprofile:\n  model: opus\n", "", "")

	_, err := cfg.GitIdentity()

	assertErrorMentions(t, err, "git.name がどのスコープにも無い")
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
	if identity(t, cfg) != (Identity{Name: "probe-user", Email: "probe@example.com"}) || *cfg.Profile.Model != "sonnet" {
		t.Errorf("Load() = git %v model %q, want user identity and repo model", identity(t, cfg), *cfg.Profile.Model)
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

func TestLoadRejectsAUserPathWhoseSymlinkTargetIsGone(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "config.yaml")
	if err := os.Symlink(filepath.Join(dir, "moved-away.yaml"), userPath); err != nil {
		t.Fatal(err)
	}

	_, err := Load(userPath, filepath.Join(dir, "sbxr.yaml"))

	assertErrorMentions(t, err, userPath)
}

func TestLoadRejectsAnEmptyPath(t *testing.T) {
	_, err := Load("", filepath.Join(t.TempDir(), "sbxr.yaml"))

	assertErrorMentions(t, err, "path が空")
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
	if identity(t, cfg) != (Identity{Name: "probe-user", Email: "probe@example.com"}) {
		t.Errorf("Git = %v, want the identity to come from the user scope only", identity(t, cfg))
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadTrustedNeedsNoGitIdentity(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, userPath, "version: 1\n")

	_, err := LoadTrusted(userPath)

	if err != nil {
		t.Errorf("LoadTrusted() error = %v, want no git identity to be required", err)
	}
}

func TestLoadTrustedStacksTheUserExclusionOnTheDefaultGroup(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, userPath, "version: 1\negress:\n  github:\n    enabled: false\n")

	cfg, err := LoadTrusted(userPath)

	if err != nil {
		t.Fatalf("LoadTrusted() error = %v", err)
	}
	if slices.Contains(cfg.GlobalEgress, "github.com:443") {
		t.Errorf("GlobalEgress = %v, want the embedded github group excluded by the user", cfg.GlobalEgress)
	}
}

func TestEmbeddedDefaultLoadsWithoutAUserConfig(t *testing.T) {
	if _, err := LoadTrusted(filepath.Join(t.TempDir(), "no-user-config.yaml")); err != nil {
		t.Errorf("LoadTrusted() error = %v", err)
	}
}

func TestEmbeddedDefaultShipsTheKnownEgressGroups(t *testing.T) {
	decl := embeddedDefault(t)

	want := []string{"cert-validation", "docker-registry", "github", "mise-tools", "ubuntu-apt"}
	if got := slices.Sorted(maps.Keys(decl.Egress)); !reflect.DeepEqual(got, want) {
		t.Errorf("default groups = %v, want %v", got, want)
	}
}

func TestEmbeddedDefaultShipsTheGitHubSecretDefinitionThatSecretSetupWrites(t *testing.T) {
	decl := embeddedDefault(t)

	if _, ok := decl.SecretDefs[secret.GitHubName]; !ok {
		t.Errorf("SecretDefs = %v, want %s", decl.SecretDefs, secret.GitHubName)
	}
}

func embeddedDefault(t *testing.T) Declaration {
	t.Helper()
	decl, err := Parse(ScopeDefault, "同梱の default 宣言", assets.DefaultDeclaration)
	if err != nil {
		t.Fatal(err)
	}
	return decl
}

func TestAnUpperScopeReplacesASecretDefinitionWhole(t *testing.T) {
	_, err := mergeYAML(t, testDefault, "version: 1\nsecret_defs:\n  github:\n    key: OTHER_TOKEN\n", "")

	assertErrorMentions(t, err, "secret_defs.github: hosts が空")
}

func TestUserMayWriteAnEmptyServiceToMeanPlaceholderInjection(t *testing.T) {
	user := "version: 1\nsecret_defs:\n  api:\n    service: ''\n    key: API_TOKEN\n    hosts: [api.example.com]\n    env: API_TOKEN\n"

	if _, err := mergeYAML(t, testDefault, user, ""); err != nil {
		t.Errorf("merge error = %v, want an empty service accepted as placeholder injection", err)
	}
}

// --- 検証は 2 段: ファイルごとの書式と、merge の後の group の中身 (ADR 0004 の改訂) ---

func TestAnInvalidAllowOfTheRepoDeclarationNamesTheFileAndScope(t *testing.T) {
	dir := t.TempDir()
	userPath, repoPath := filepath.Join(dir, "config.yaml"), filepath.Join(dir, "sbxr.yaml")
	writeFile(t, userPath, "version: 1\n")
	writeFile(t, repoPath, "version: 1\negress:\n  api:\n    rationale: API\n    allow: [https://api.example.com]\n")

	_, err := Load(userPath, repoPath)

	assertErrorMentions(t, err, repoPath+" (repo スコープ)")
	assertErrorMentions(t, err, "egress.api: allow の \"https://api.example.com\"")
}

func TestExcludingAGroupThatDoesNotExistNamesTheScopeThatWroteIt(t *testing.T) {
	_, err := mergeYAML(t, testDefault, "version: 1\negress:\n  githb:\n    enabled: false\n", "")

	assertErrorMentions(t, err, "egress.githb (user スコープが書いた group): rationale が空")
}

func TestAGroupCompletedByAnUpperScopeIsValid(t *testing.T) {
	cfg := mustMerge(t, "version: 1\negress:\n  api:\n    rationale: API\n", "version: 1\negress:\n  api:\n    allow: [api.example.com:443]\n", "")

	if !slices.Equal(cfg.GlobalEgress, []string{"api.example.com:443"}) {
		t.Errorf("GlobalEgress = %v, want the group completed across the scopes", cfg.GlobalEgress)
	}
}

func TestRepoCannotExcludeEgressGroups(t *testing.T) {
	_, err := mergeYAML(t, testDefault, "", "version: 1\negress:\n  github:\n    enabled: false\n")

	assertErrorMentions(t, err, "egress.github.enabled")
}

// --- スコープ制限の表はすべての深さで引く (ADR 0004 の改訂) ---

func TestRepoWritesAnythingBelowAKeyTheTableAllowsWithItsSubtree(t *testing.T) {
	for name, repo := range map[string]string{
		"profile.enabledPlugins": "version: 1\nprofile:\n  enabledPlugins:\n    repo-plugin@my.mk: true\n",
		"egress.*.allow":         "version: 1\negress:\n  api:\n    rationale: API\n    allow: [api.example.com:443]\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(ScopeRepo, "repo", []byte(repo)); err != nil {
				t.Errorf("Parse() error = %v, want the subtree written", err)
			}
		})
	}
}

func TestRepoCannotHideAKeyBehindAnAlias(t *testing.T) {
	for name, tt := range map[string]struct{ repo, needle string }{
		"値を alias にする": {
			repo:   "version: 1\nprofile:\n  enabledPlugins: &hidden {enabled: true}\negress:\n  github: *hidden\n",
			needle: "egress.github.enabled は repo 宣言には書けない",
		},
		"top-level の key を alias にする": {
			repo:   "version: 1\nprofile:\n  model: &herdr herdr\n*herdr : {enabled: true}\n",
			needle: "herdr は repo 宣言には書けない",
		},
		"group の下の key を alias にする": {
			repo:   "version: 1\negress:\n  api:\n    rationale: &allow enabled\n    allow: [api.example.com:443]\n  github: {*allow : false}\n",
			needle: "egress.github.enabled は repo 宣言には書けない",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(ScopeRepo, "repo", []byte(tt.repo))

			assertErrorMentions(t, err, tt.needle)
		})
	}
}

func TestRepoCannotMergeInAKeyTheTableForbids(t *testing.T) {
	for name, repo := range map[string]string{
		"top-level": "version: 1\n<<: {herdr: {enabled: true}}\n",
		"group の下":  "version: 1\negress:\n  github:\n    <<: {enabled: false}\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(ScopeRepo, "repo", []byte(repo))

			assertErrorMentions(t, err, "<< は repo 宣言には書けない")
		})
	}
}

func TestRepoIsToldOnceAboutAKeyItCannotWriteWithoutItsChildren(t *testing.T) {
	_, err := Parse(ScopeRepo, "repo", []byte("version: 1\nprofile:\n  env:\n    A: x\n    B: y\n"))

	if err == nil || strings.Count(err.Error(), "repo 宣言には書けない") != 1 {
		t.Errorf("error = %v, want profile.env reported once", err)
	}
}

// --- herdr 連携 (ADR 0007) ---

func TestRepoCannotDeclareHerdr(t *testing.T) {
	_, err := mergeYAML(t, testDefault, "", "version: 1\nherdr:\n  enabled: true\n")

	assertErrorMentions(t, err, "herdr")
}

func TestHerdrIsOffUnlessTheUserEnablesIt(t *testing.T) {
	cfg := mustMerge(t, testDefault+"herdr:\n  enabled: false\n  version: v0.9.0\n", "", "")

	if cfg.Herdr.Enabled {
		t.Errorf("Herdr = %+v, want it disabled by default", cfg.Herdr)
	}
}

func TestUserEnablesHerdrWithTheDefaultVersion(t *testing.T) {
	cfg := mustMerge(t, testDefault+"herdr:\n  enabled: false\n  version: v0.9.0\n", "version: 1\nherdr:\n  enabled: true\n", "")

	if cfg.Herdr != (Herdr{Enabled: true, Version: "v0.9.0"}) {
		t.Errorf("Herdr = %+v, want enabled with the default version", cfg.Herdr)
	}
}

func TestHerdrVersionMustBeAReleaseTag(t *testing.T) {
	_, err := mergeYAML(t, testDefault, "version: 1\nherdr:\n  enabled: true\n  version: latest; rm -rf /\n", "")

	assertErrorMentions(t, err, "herdr.version")
}

func TestEnablingHerdrWithoutAnyVersionIsAnError(t *testing.T) {
	_, err := mergeYAML(t, testDefault, "version: 1\nherdr:\n  enabled: true\n", "")

	assertErrorMentions(t, err, "herdr.version")
}

func TestEmbeddedDefaultShipsHerdrDisabledWithAPinnedVersion(t *testing.T) {
	decl, err := Parse(ScopeDefault, "default", assets.DefaultDeclaration)

	if err != nil {
		t.Fatal(err)
	}
	if decl.Herdr.Enabled == nil || *decl.Herdr.Enabled || decl.Herdr.Version == nil || *decl.Herdr.Version != "v0.9.0" {
		t.Errorf("default herdr = %+v, want enabled: false and version v0.9.0", decl.Herdr)
	}
}
