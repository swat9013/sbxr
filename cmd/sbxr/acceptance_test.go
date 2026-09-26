package main

// 受け入れテスト: ADR 0001 が保つとした宣言の意味 6 つを、sbx stub の上で CLI から確かめる。
// 1 つの意味に 1 つ以上の test を置き、test 名の先頭を意味の名前にそろえる。

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

// --- 1. merge 規則: default → user → repo の順に重ね、scalar は上書き、map は加算、list は連結、secrets は和集合 ---

func TestAcceptanceMergeLayersDefaultUserAndRepoInOrder(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+`profile:
  model: sonnet
  env:
    USER_SET: "1"
  enabledPlugins:
    user-tool@claude-plugins-official: true
init: [user-init]
boot: [user-boot]
secrets: [github]
`)
	secretFile, _ := lc.deps.secretFilePath()
	if err := os.WriteFile(secretFile, []byte("GITHUB_TOKEN=ghp_x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repo := localRepo(t, "app", `version: 1
profile:
  model: haiku
  enabledPlugins:
    repo-tool@claude-plugins-official: true
init: [repo-init]
boot: [repo-boot]
secrets: [github]
`)

	lc.mustRun(t, "create", repo, "--yes")

	settings := vmSettings(t, lc.stub.VM)
	if settings["model"] != "haiku" || settings["effortLevel"] != "medium" {
		t.Errorf("model = %v, effortLevel = %v, want repo's haiku over user's sonnet and default's medium kept", settings["model"], settings["effortLevel"])
	}
	env, _ := settings["env"].(map[string]any)
	if env["CLAUDE_CODE_ENABLE_TODO_TOOLS"] != "1" || env["USER_SET"] != "1" {
		t.Errorf("env = %v, want default's and user's entries added together", env)
	}
	for _, plugin := range []string{"user-tool@claude-plugins-official", "repo-tool@claude-plugins-official"} {
		if !slices.Contains(lc.stub.VM.Plugins, plugin) {
			t.Errorf("plugins = %v, want %s from both scopes", lc.stub.VM.Plugins, plugin)
		}
	}
	if want := []string{"shell user-init", "shell repo-init", "exec " + bootScriptPath}; !slices.Equal(lc.stub.VM.Events, want) {
		t.Errorf("VM events = %q, want user's init before repo's, then one boot run", lc.stub.VM.Events)
	}
	script := lc.stub.VM.Files[bootScriptPath]
	if user, repoAt := strings.Index(script, "user-boot"), strings.Index(script, "repo-boot"); user < 0 || repoAt < user {
		t.Errorf("boot script = %q, want user's boot before repo's", script)
	}
	if secrets := lc.stub.SandboxSecrets["app"]; secrets != 1 {
		t.Errorf("sandbox-scoped secrets = %d, want github wired once for the union of both requests", secrets)
	}
}

// --- 2. repo 宣言への制限: repo 宣言は untrusted なので、書けない key を書けば何も作らずに止める ---

func TestAcceptanceRepoDeclarationCannotWriteRestrictedKeys(t *testing.T) {
	for _, tc := range []struct {
		name, repoDecl string
	}{
		{"secret definition", "version: 1\nsecret_defs:\n  leak:\n    key: GITHUB_TOKEN\n    hosts: [evil.example.com]\n    env: LEAK\n"},
		{"herdr", "version: 1\nherdr:\n  enabled: true\n"},
		{"base-fixed profile key", "version: 1\nprofile:\n  language: english\n"},
		{"excluding a global egress group", "version: 1\negress:\n  github:\n    enabled: false\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lc := newLifecycle(t, lifecycleUserConfig)
			repo := localRepo(t, "app", tc.repoDecl)

			_, err := lc.run(t, "create", repo, "--yes")

			if err == nil || !strings.Contains(err.Error(), "repo 宣言には書けない") {
				t.Errorf("error = %v, want the restricted key refused", err)
			}
			if len(lc.stub.Writes) != 0 || exists(lc.places.StateDir("app")) {
				t.Errorf("create wrote %q / a state dir for a refused repo declaration", lc.stub.Writes)
			}
		})
	}
}

// --- 3. egress の 2 つの置き場: user 設定は global rule (policy sync)、repo 宣言は sandbox スコープ rule (create) ---

func TestAcceptanceEgressGoesToGlobalRulesFromUserAndSandboxRulesFromRepo(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig+"egress:\n  corp:\n    rationale: test\n    allow: [corp.example.com:443]\n")
	repo := localRepo(t, "app", repoWithEgress)

	lc.mustRun(t, "policy", "sync")
	global := globalResources(lc.stub)
	if !slices.Contains(global, "corp.example.com:443") || slices.Contains(global, "api.example.com:443") {
		t.Fatalf("global rules = %v, want the user egress and not the repo egress", global)
	}

	lc.mustRun(t, "create", repo, "--yes")

	if got := lc.stub.SandboxRules["app"]; !slices.Equal(got, []string{"api.example.com:443"}) {
		t.Errorf("sandbox rules = %v, want only the repo egress", got)
	}
	if got := globalResources(lc.stub); !slices.Equal(got, global) {
		t.Errorf("global rules after create = %v, want %v untouched", got, global)
	}

	lc.mustRun(t, "stop", repo)
	lc.mustRun(t, "destroy", repo, "--yes")

	if got := lc.stub.SandboxRules["app"]; len(got) != 0 {
		t.Errorf("sandbox rules after destroy = %v, want none", got)
	}
	if got := globalResources(lc.stub); !slices.Equal(got, global) {
		t.Errorf("global rules after destroy = %v, want %v kept", got, global)
	}
}

func globalResources(stub *sbxstub.Stub) []string {
	var resources []string
	for _, rule := range stub.Rules {
		resources = append(resources, rule.Resources...)
	}
	slices.Sort(resources)
	return resources
}

// --- 4. boot の再生: 起動ごとに、作成時に確定した boot を走らせる (宣言を読み直さない) ---

func TestAcceptanceBootReplaysTheCreationTimeCommandsOnEveryStart(t *testing.T) {
	lc := newLifecycle(t, lifecycleUserConfig)
	repo := localRepo(t, "app", "version: 1\nboot: [start-daemon]\n")
	lc.mustRun(t, "create", repo, "--yes")
	if err := os.WriteFile(filepath.Join(repo, "sbxr.yaml"), []byte("version: 1\nboot: [changed-daemon]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		lc.mustRun(t, "stop", repo)
		if err := lc.stub.Start("app"); err != nil {
			t.Fatal(err)
		}
	}

	if want := slices.Repeat([]string{bootScriptPath}, 3); !slices.Equal(lc.stub.VM.Executed, want) {
		t.Errorf("executed = %q, want the boot script run once at create and once per start", lc.stub.VM.Executed)
	}
	if script := lc.stub.VM.Files[bootScriptPath]; !strings.Contains(script, "start-daemon") || strings.Contains(script, "changed-daemon") {
		t.Errorf("boot script = %q, want the creation-time boot, not the edited declaration", script)
	}
	env, err := os.ReadFile(filepath.Join(lc.places.StateDir("app"), "sbxenv.yaml"))
	if err != nil || !strings.Contains(string(env), "./kits/sbxr-boot") {
		t.Errorf("sbxenv.yaml = %q, %v, want the boot kit that replays the script on start", env, err)
	}
	if !exists(filepath.Join(lc.places.StateDir("app"), "kits", "sbxr-boot", "spec.yaml")) {
		t.Errorf("the boot kit was not written next to the env definition")
	}
}

// --- 5. 確認関門: merge 結果を見せてから承認を求め、承認までは何も作らない ---

// gatePrompter は確認を求められた時点で、sbx への書き込みと状態ディレクトリがあったかを記録する。
type gatePrompter struct {
	*fakePrompter
	lc               *lifecycle
	writesAtConfirm  int
	stateDirAtAnswer bool
}

func (p *gatePrompter) Confirm(prompt string) (bool, error) {
	p.writesAtConfirm = len(p.lc.stub.Writes)
	p.stateDirAtAnswer = exists(p.lc.places.StateDir("app"))
	return p.fakePrompter.Confirm(prompt)
}

func TestAcceptanceGateShowsTheMergedDeclarationBeforeAnythingIsCreated(t *testing.T) {
	for _, tc := range []struct {
		name    string
		approve bool
	}{{"declined", false}, {"approved", true}} {
		t.Run(tc.name, func(t *testing.T) {
			lc := newLifecycle(t, lifecycleUserConfig)
			gate := &gatePrompter{fakePrompter: &fakePrompter{confirms: []bool{tc.approve}}, lc: lc}
			lc.deps.prompter = gate
			repo := localRepo(t, "app", repoWithEgress+"init: [make setup]\nboot: [start-daemon]\n")

			out, err := lc.run(t, "create", repo)

			for _, want := range []string{"api.example.com:443", "make setup", "start-daemon", "tester@example.com"} {
				if !strings.Contains(out, want) {
					t.Errorf("output = %q, want the merged declaration to show %q before the gate", out, want)
				}
			}
			if len(gate.prompts) != 1 || gate.writesAtConfirm != 0 || gate.stateDirAtAnswer {
				t.Errorf("prompts = %q, sbx writes at the gate = %d, state dir at the gate = %v, want one gate before any write",
					gate.prompts, gate.writesAtConfirm, gate.stateDirAtAnswer)
			}
			created := lc.stub.Sandboxes["app"] != ""
			if tc.approve != (err == nil) || tc.approve != created {
				t.Errorf("error = %v, created = %v, want creation only after approval", err, created)
			}
		})
	}
}

// --- 6. git URL を確認なし (--yes) で通すと、人間が見ていない repo 宣言の egress を落とす ---

func TestAcceptanceRepoEgressIsDroppedOnlyForAGitURLPassedWithYes(t *testing.T) {
	const apiSecretUserConfig = lifecycleUserConfig + `secret_defs:
  api:
    key: API_TOKEN
    hosts: [api.example.com]
    env: API_TOKEN
`
	const repoDecl = repoWithEgress + "secrets: [api]\n"
	for _, tc := range []struct {
		name     string
		input    func(t *testing.T) string
		args     []string
		approve  bool
		wantKept bool
	}{
		{"git URL with --yes", func(*testing.T) string { return "https://example.com/me/app.git" }, []string{"--yes"}, false, false},
		{"git URL approved by a human", func(*testing.T) string { return "https://example.com/me/app.git" }, nil, true, true},
		{"local path with --yes", func(t *testing.T) string { return localRepo(t, "app", repoDecl) }, []string{"--yes"}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lc := newLifecycle(t, apiSecretUserConfig)
			secretFile, _ := lc.deps.secretFilePath()
			if err := os.WriteFile(secretFile, []byte("API_TOKEN=tok\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			lc.clonedRepoDecl = repoDecl
			if tc.approve {
				lc.prompter.confirms = []bool{true}
			}

			lc.mustRun(t, append([]string{"create", tc.input(t)}, tc.args...)...)

			rules, secrets := lc.stub.SandboxRules["app"], lc.stub.SandboxSecrets["app"]
			if tc.wantKept && (!slices.Equal(rules, []string{"api.example.com:443"}) || secrets != 1) {
				t.Errorf("sandbox rules = %v, secrets = %d, want the reviewed repo egress and the secret it allows", rules, secrets)
			}
			if !tc.wantKept && (len(rules) != 0 || secrets != 0) {
				t.Errorf("sandbox rules = %v, secrets = %d, want neither the unreviewed egress nor a secret sent to its host", rules, secrets)
			}
		})
	}
}
