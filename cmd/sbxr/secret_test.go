package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/secret"
)

// fakeGitHub は GitHub API の private repo 1 つを再現する。status は path ごとの応答で、無い path は 404 を返す。
type fakeGitHub struct {
	responses map[string]fakeResponse
	// tokens は受け取った Authorization header。
	tokens []string
}

type fakeResponse struct {
	status int
	body   any
}

var deniedByToken = fakeResponse{http.StatusForbidden, map[string]string{"message": "Resource not accessible by personal access token"}}

// recommendedToken は推奨権限 (Contents・Issues・Pull requests の RW、Metadata の R) の token への応答。
func recommendedToken() map[string]fakeResponse {
	return map[string]fakeResponse{
		"/repos/me/private":                   {http.StatusOK, map[string]any{"private": true}},
		"/repos/me/private/contents/":         {http.StatusOK, []any{}},
		"/repos/me/private/actions/secrets":   deniedByToken,
		"/repos/me/private/actions/workflows": deniedByToken,
		"/repos/me/private/keys":              deniedByToken,
	}
}

func (f *fakeGitHub) serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.tokens = append(f.tokens, r.Header.Get("Authorization"))
		response, ok := f.responses[r.URL.Path]
		if !ok {
			response = fakeResponse{http.StatusNotFound, map[string]string{"message": "Not Found"}}
		}
		w.WriteHeader(response.status)
		_ = json.NewEncoder(w).Encode(response.body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// fakePrompter は入力を順に返し、受け取った prompt を記録する。
type fakePrompter struct {
	lines    []string
	hidden   []string
	confirms []bool
	// noTerminal なら Confirm は端末が無いときと同じ error を返す。
	noTerminal bool
	prompts    []string
}

func (p *fakePrompter) Confirm(prompt string) (bool, error) {
	p.prompts = append(p.prompts, prompt)
	if p.noTerminal {
		return false, errors.New("確認には端末が要る (stdin が端末でない)")
	}
	answer := p.confirms[0]
	p.confirms = p.confirms[1:]
	return answer, nil
}

func (p *fakePrompter) Line(prompt string) (string, error) {
	p.prompts = append(p.prompts, prompt)
	line := p.lines[0]
	p.lines = p.lines[1:]
	return line, nil
}

func (p *fakePrompter) Hidden(prompt string) (string, error) {
	p.prompts = append(p.prompts, prompt)
	value := p.hidden[0]
	p.hidden = p.hidden[1:]
	return value, nil
}

func setupDeps(t *testing.T, apiBase string, prompter *fakePrompter) (dependencies, string) {
	t.Helper()
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "secrets.env")
	return dependencies{
		userConfigPath: fixedPath(filepath.Join(dir, "config.yaml")),
		secretFilePath: fixedPath(secretFile),
		githubAPI:      apiBase,
		prompter:       prompter,
	}, secretFile
}

func TestSecretSetupGithubWritesATokenThatPassesEveryProbe(t *testing.T) {
	github := &fakeGitHub{responses: recommendedToken()}
	prompter := &fakePrompter{lines: []string{"me/private"}, hidden: []string{"ghp_good"}}
	deps, secretFile := setupDeps(t, github.serve(t), prompter)

	out, err := runSbxr(t, deps, "secret", "setup", "github")

	if err != nil {
		t.Fatalf("secret setup github error = %v, output = %q", err, out)
	}
	values, err := secret.ReadFile(secretFile)
	if err != nil || values["GITHUB_TOKEN"] != "ghp_good" {
		t.Errorf("secret file = %v, %v, want GITHUB_TOKEN=ghp_good", values, err)
	}
}

func TestSecretSetupGithubProbesWithTheEnteredToken(t *testing.T) {
	github := &fakeGitHub{responses: recommendedToken()}
	prompter := &fakePrompter{lines: []string{"me/private"}, hidden: []string{"ghp_good"}}
	deps, _ := setupDeps(t, github.serve(t), prompter)

	if _, err := runSbxr(t, deps, "secret", "setup", "github"); err != nil {
		t.Fatalf("secret setup github error = %v", err)
	}

	for _, token := range github.tokens {
		if token != "Bearer ghp_good" {
			t.Errorf("Authorization = %q, want the entered token", token)
		}
	}
}

func TestSecretSetupGithubShowsThePermissionsBeforeAskingForInput(t *testing.T) {
	github := &fakeGitHub{responses: recommendedToken()}
	prompter := &fakePrompter{lines: []string{"me/private"}, hidden: []string{"ghp_good"}}
	deps, _ := setupDeps(t, github.serve(t), prompter)

	out, err := runSbxr(t, deps, "secret", "setup", "github")

	if err != nil {
		t.Fatalf("secret setup github error = %v", err)
	}
	for _, permission := range []string{"Contents", "Issues", "Pull requests", "Metadata", "Secrets", "Actions", "Workflows", "Administration"} {
		if !strings.Contains(out, permission) {
			t.Errorf("output = %q, want it to name the %s permission", out, permission)
		}
	}
}

func TestSecretSetupGithubDoesNotWriteATokenThatFailsAProbe(t *testing.T) {
	for name, override := range map[string]map[string]fakeResponse{
		"Actions secrets を読める (過剰権限)":   {"/repos/me/private/actions/secrets": {http.StatusOK, map[string]any{"total_count": 0}}},
		"Actions workflows を読める (過剰権限)": {"/repos/me/private/actions/workflows": {http.StatusOK, map[string]any{"total_count": 0}}},
		"deploy keys を読める (過剰権限)":       {"/repos/me/private/keys": {http.StatusOK, []any{}}},
		"Contents を読めない":                {"/repos/me/private/contents/": deniedByToken},
		"rate limit の 403 (判定できない)":     {"/repos/me/private/keys": {http.StatusForbidden, map[string]string{"message": "API rate limit exceeded"}}},
		"5xx (判定できない)":                  {"/repos/me/private/actions/secrets": {http.StatusBadGateway, map[string]string{"message": "Server Error"}}},
		"public repo (probe にならない)":     {"/repos/me/private": {http.StatusOK, map[string]any{"private": false}}},
		"repo が見えない":                    {"/repos/me/private": {http.StatusNotFound, map[string]string{"message": "Not Found"}}},
	} {
		t.Run(name, func(t *testing.T) {
			responses := recommendedToken()
			for path, response := range override {
				responses[path] = response
			}
			github := &fakeGitHub{responses: responses}
			prompter := &fakePrompter{lines: []string{"me/private"}, hidden: []string{"ghp_bad"}}
			deps, secretFile := setupDeps(t, github.serve(t), prompter)

			_, err := runSbxr(t, deps, "secret", "setup", "github")

			if err == nil {
				t.Errorf("secret setup github error = nil, want the probe failure to exit non-zero")
			}
			if _, statErr := os.Stat(secretFile); statErr == nil {
				t.Errorf("secret file was written despite the failed probe")
			}
		})
	}
}

func TestSecretSetupGithubRejectsARepoThatIsNotOwnerSlashName(t *testing.T) {
	prompter := &fakePrompter{lines: []string{"https://github.com/me/private"}, hidden: []string{"ghp_good"}}
	deps, _ := setupDeps(t, "http://127.0.0.1:0", prompter)

	_, err := runSbxr(t, deps, "secret", "setup", "github")

	if err == nil || !strings.Contains(err.Error(), "owner/name") {
		t.Errorf("error = %v, want it to ask for owner/name", err)
	}
}

func TestSecretSetupCustomWritesTheKeyOfTheDefinitionForTheHostWithoutProbing(t *testing.T) {
	prompter := &fakePrompter{hidden: []string{"glpat-value"}}
	deps, secretFile := setupDeps(t, "http://127.0.0.1:0", prompter)
	userConfig := "version: 1\nsecret_defs:\n  gitlab:\n    key: GITLAB_TOKEN\n    hosts: [gitlab.example.com]\n    env: GITLAB_TOKEN\n"
	path, _ := deps.userConfigPath()
	if err := os.WriteFile(path, []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := runSbxr(t, deps, "secret", "setup", "custom", "--host", "gitlab.example.com")

	if err != nil {
		t.Fatalf("secret setup custom error = %v, output = %q", err, out)
	}
	if !strings.Contains(out, "能力は確認していない") {
		t.Errorf("output = %q, want it to say the capability is not checked", out)
	}
	values, err := secret.ReadFile(secretFile)
	if err != nil || values["GITLAB_TOKEN"] != "glpat-value" {
		t.Errorf("secret file = %v, %v, want GITLAB_TOKEN=glpat-value", values, err)
	}
}

func TestSecretSetupCustomStopsWhenNoDefinitionInjectsIntoTheHost(t *testing.T) {
	prompter := &fakePrompter{hidden: []string{"value"}}
	deps, secretFile := setupDeps(t, "http://127.0.0.1:0", prompter)

	_, err := runSbxr(t, deps, "secret", "setup", "custom", "--host", "unknown.example.com")

	if err == nil || !strings.Contains(err.Error(), "unknown.example.com") {
		t.Errorf("error = %v, want it to name the host without a definition", err)
	}
	if len(prompter.prompts) != 0 {
		t.Errorf("prompts = %q, want no input asked before the definition is found", prompter.prompts)
	}
	if _, statErr := os.Stat(secretFile); statErr == nil {
		t.Errorf("secret file was written without a definition")
	}
}

func TestSecretSetupCustomStopsWhenDefinitionsForTheHostUseDifferentKeys(t *testing.T) {
	prompter := &fakePrompter{hidden: []string{"value"}}
	deps, secretFile := setupDeps(t, "http://127.0.0.1:0", prompter)
	userConfig := "version: 1\nsecret_defs:\n" +
		"  a:\n    key: A_TOKEN\n    hosts: [shared.example.com]\n    env: A_TOKEN\n" +
		"  b:\n    key: B_TOKEN\n    hosts: [shared.example.com]\n    env: B_TOKEN\n"
	path, _ := deps.userConfigPath()
	if err := os.WriteFile(path, []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := runSbxr(t, deps, "secret", "setup", "custom", "--host", "shared.example.com")

	if err == nil || !strings.Contains(err.Error(), "A_TOKEN") || !strings.Contains(err.Error(), "B_TOKEN") {
		t.Errorf("error = %v, want it to name both keys", err)
	}
	if _, statErr := os.Stat(secretFile); statErr == nil {
		t.Errorf("secret file was written while the key is ambiguous")
	}
}
