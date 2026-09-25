package secret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// gitHubRepoPattern は probe に使う repo の書式 (owner/name)。
var gitHubRepoPattern = regexp.MustCompile(`^[A-Za-z0-9-]+/[A-Za-z0-9._-]+$`)

// ValidateGitHubRepo は probe に使う repo が owner/name の形かを確かめる。
func ValidateGitHubRepo(repo string) error {
	if !gitHubRepoPattern.MatchString(repo) {
		return fmt.Errorf("repo %q は owner/name の形で書く", repo)
	}
	return nil
}

// deniedByTokenMessage は fine-grained PAT が権限の無い API を叩いたときに GitHub が返す文言。
// 403 は rate limit でも返るので、status ではなくこの文言で拒否を判定する。
const deniedByTokenMessage = "Resource not accessible by personal access token"

// GitHubProbe は token の能力を GitHub API で確かめる。
type GitHubProbe struct {
	// BaseURL は GitHub API の root (https://api.github.com)。
	BaseURL string
	Client  *http.Client
}

// forbiddenCapabilities は token に付けてはならない権限と、それを確かめる読み取りの API。
// どれか 1 つでも通れば過剰権限として止める。permission は fine-grained PAT の画面での権限名。
// .github/workflows を書き換える Workflows 権限は書き込み専用で、読み取りの API では確かめられない。
var forbiddenCapabilities = []struct {
	permission string
	path       string
}{
	{"Secrets", "/actions/secrets"},
	{"Actions", "/actions/workflows"},
	{"Administration", "/keys"},
}

// ForbiddenPermissions は probe で拒否を確かめる権限の名前と、確かめる API (例: "Actions (GET /actions/workflows)")。
func ForbiddenPermissions() []string {
	names := make([]string, 0, len(forbiddenCapabilities))
	for _, capability := range forbiddenCapabilities {
		names = append(names, fmt.Sprintf("%s (GET %s)", capability.permission, capability.path))
	}
	return names
}

// Verify は repo (private) に対して、Contents を読めることと、付けてはならない権限がすべて拒否されることを確かめる。
// 確かめられなければ (拒否でも許可でもない応答) 止める。
func (p GitHubProbe) Verify(ctx context.Context, repo, token string) error {
	if err := ValidateGitHubRepo(repo); err != nil {
		return err
	}
	base := "/repos/" + repo
	var metadata struct {
		Private *bool `json:"private"`
	}
	if err := p.expectAllowed(ctx, token, base, &metadata, "repo の Metadata"); err != nil {
		return err
	}
	if metadata.Private == nil || !*metadata.Private {
		return fmt.Errorf("%s は private repo でない (public repo は権限が無くても読めるので probe にならない)", repo)
	}
	// contents/ は commit の無い repo では 404 を返すので、probe の repo には commit が要る
	if err := p.expectAllowed(ctx, token, base+"/contents/", nil, "Contents"); err != nil {
		return err
	}
	var errs []error
	for _, capability := range forbiddenCapabilities {
		outcome, err := p.call(ctx, token, base+capability.path)
		switch {
		case err != nil:
			return fmt.Errorf("%s 権限を判定できない: %w", capability.permission, err)
		case outcome.allowed():
			errs = append(errs, fmt.Errorf("%s 権限が付いている (過剰権限。token から外す)", capability.permission))
		case !outcome.deniedByToken():
			return fmt.Errorf("%s 権限を判定できない: %s", capability.permission, outcome)
		}
	}
	return errors.Join(errs...)
}

func (p GitHubProbe) expectAllowed(ctx context.Context, token, path string, into any, what string) error {
	outcome, err := p.call(ctx, token, path)
	switch {
	case err != nil:
		return fmt.Errorf("%s を確かめられない: %w", what, err)
	case outcome.deniedByToken():
		return fmt.Errorf("%s を読めない (token に権限を付ける)", what)
	case !outcome.allowed():
		return fmt.Errorf("%s を確かめられない: %s", what, outcome)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(outcome.body, into); err != nil {
		return fmt.Errorf("%s の応答を読めない: %w", what, err)
	}
	return nil
}

// outcome は GitHub API の 1 回の応答。
type outcome struct {
	status  int
	message string
	body    []byte
}

func (o outcome) allowed() bool { return o.status >= 200 && o.status < 300 }

func (o outcome) deniedByToken() bool {
	return o.status == http.StatusForbidden && strings.Contains(o.message, deniedByTokenMessage)
}

func (o outcome) String() string {
	return fmt.Sprintf("HTTP %d %s", o.status, o.message)
}

func (p GitHubProbe) call(ctx context.Context, token, path string) (outcome, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(p.BaseURL, "/")+path, nil)
	if err != nil {
		return outcome{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := p.Client.Do(req)
	if err != nil {
		return outcome{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return outcome{}, err
	}
	result := outcome{status: resp.StatusCode, body: body}
	var errorBody struct {
		Message string `json:"message"`
	}
	if !result.allowed() && json.Unmarshal(body, &errorBody) == nil {
		result.message = errorBody.Message
	}
	return result, nil
}
