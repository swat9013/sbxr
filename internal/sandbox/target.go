// Package sandbox は sandbox VM のライフサイクル (plan / create / destroy / stop) を組み立てる。
package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
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

// namePattern は sandbox VM の名前。状態ディレクトリと cache clone の path の 1 要素になるので、区切りと相対指定を通さない。
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// IsGitURL は <repo> を git URL として扱うかを返す。
func IsGitURL(input string) bool {
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://", "git@"} {
		if strings.HasPrefix(input, prefix) {
			return true
		}
	}
	return false
}

// ResolveTarget は <repo> (path | git URL) を Target にする。clone はしない (destroy と stop が network に出ないように)。
// git URL の Repo は cacheRoot/<name>。
func ResolveTarget(input, cacheRoot string) (Target, error) {
	if IsGitURL(input) {
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
	info, err := os.Stat(repo)
	if err != nil || !info.IsDir() {
		return Target{}, fmt.Errorf("repo のディレクトリ %s が無い", input)
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

// ExecClone は host の gh (GitHub) / glab (GitLab) / git (その他) で clone する。host 側の CLI の認証を使う。
// URL は argv でそのまま渡し、shell を通さない。stdin は渡さない (null device になる)。
func ExecClone(ctx context.Context, url, dir string) error {
	var args []string
	switch {
	case strings.Contains(url, "github.com"):
		args = []string{"gh", "repo", "clone", url, dir}
	case strings.Contains(url, "gitlab"):
		args = []string{"glab", "repo", "clone", url, dir}
	default:
		args = []string{"git", "clone", url, dir}
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// ensureClone は git URL の Target の cache clone を用意する。既にあれば使い回す。
func ensureClone(ctx context.Context, clone Cloner, target Target) error {
	if !target.FromGitURL() {
		return nil
	}
	if _, err := os.Stat(target.Repo); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target.Repo), 0o700); err != nil {
		return err
	}
	return clone(ctx, target.URL, target.Repo)
}
