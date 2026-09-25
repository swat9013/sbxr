// Package sandbox は sandbox VM のライフサイクル (plan / create / destroy / stop) を組み立てる。
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Target は <repo> 引数を解決した結果。
type Target struct {
	// Name は sandbox VM の名前。状態ディレクトリと cache clone の名前にも使う。
	Name string
	// Repo は sbx に渡す host 側の repo のディレクトリ (git URL なら cache clone)。
	Repo string
	// URL は <repo> が git URL のときの URL。path のときは空。
	URL string
}

// FromGitURL は <repo> が git URL だったかを返す。
func (t Target) FromGitURL() bool {
	return t.URL != ""
}

// Source は sandbox VM の出所 (git URL か repo の絶対 path)。同じ名前の別 repo を取り違えないために状態ディレクトリへ記録する。
func (t Target) Source() string {
	if t.FromGitURL() {
		return t.URL
	}
	return t.Repo
}

// namePattern は sandbox VM の名前。状態ディレクトリと cache clone の path の 1 要素になるので、区切りと相対指定を通さない。
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func isGitURL(input string) bool {
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://", "git@"} {
		if strings.HasPrefix(input, prefix) {
			return true
		}
	}
	return false
}

// ResolveTarget は <repo> (path | git URL) を Target にする。clone も path の存在確認もしない
// (destroy と stop が network に出ず、host の repo を移動・削除した後でも VM を扱えるように)。
// git URL の Repo は cacheRoot/<name>。
func ResolveTarget(input, cacheRoot string) (Target, error) {
	if isGitURL(input) {
		name := strings.TrimSuffix(input, "/")
		name = name[strings.LastIndexAny(name, "/:")+1:]
		name = strings.TrimSuffix(name, ".git")
		if err := validateName(name); err != nil {
			return Target{}, fmt.Errorf("git URL %s から sandbox VM の名前を決められない: %w", input, err)
		}
		return Target{Name: name, Repo: filepath.Join(cacheRoot, name), URL: input}, nil
	}
	repo, err := filepath.Abs(input)
	if err != nil {
		return Target{}, err
	}
	name := filepath.Base(repo)
	if err := validateName(name); err != nil {
		return Target{}, fmt.Errorf("repo のディレクトリ名 %s を sandbox VM の名前にできない: %w", name, err)
	}
	return Target{Name: name, Repo: repo}, nil
}

func validateName(name string) error {
	if !namePattern.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("%q は英数字と . _ - だけで書く名前にする", name)
	}
	return nil
}

// Cloner は git URL を dir へ clone する。
type Cloner func(ctx context.Context, url, dir string) error

// ExecClone は host の gh (github.com) / glab (gitlab を含む host) / git (その他) で clone する。host 側の CLI の認証を使う。
// URL は argv でそのまま渡し、shell を通さない。stdin は渡さない (null device になる)。
func ExecClone(ctx context.Context, url, dir string) error {
	args := cloneCommand(url, dir)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// cloneCommand は URL の host で clone に使う CLI を選ぶ。
func cloneCommand(rawURL, dir string) []string {
	host := gitURLHost(rawURL)
	switch {
	case host == "github.com":
		return []string{"gh", "repo", "clone", rawURL, dir}
	case strings.Contains(host, "gitlab"):
		return []string{"glab", "repo", "clone", rawURL, dir}
	}
	return []string{"git", "clone", rawURL, dir}
}

// gitURLHost は URL 形 (https://host/...) と scp 形 (git@host:path) の host を返す。
func gitURLHost(rawURL string) string {
	if parsed, err := url.Parse(rawURL); err == nil && parsed.Host != "" {
		return strings.ToLower(parsed.Hostname())
	}
	host, _, _ := strings.Cut(rawURL, ":")
	return strings.ToLower(host[strings.LastIndex(host, "@")+1:])
}

// FreshClone は git URL の Target を dir へ clone し直す。dir に前の clone があれば消してから clone する
// (別の URL の clone や古い clone を使い回さない)。
func FreshClone(ctx context.Context, clone Cloner, target Target) error {
	if err := os.RemoveAll(target.Repo); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target.Repo), 0o700); err != nil {
		return err
	}
	if err := clone(ctx, target.URL, target.Repo); err != nil {
		return fmt.Errorf("%s を clone できない: %w", target.URL, err)
	}
	return nil
}
