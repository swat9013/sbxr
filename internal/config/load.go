package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/swat9013/sbxr/internal/assets"
)

// Load は同梱の default スコープに、userPath の user 設定と repoPath の repo 宣言を重ねる。
// ファイルが無いスコープは宣言が無いものとして扱う。
func Load(userPath, repoPath string) (Config, error) {
	user, err := ReadUserFile(userPath)
	if err != nil {
		return Config{}, err
	}
	repo, err := ReadRepoFile(repoPath)
	if err != nil {
		return Config{}, err
	}
	return user.With(repo)
}

// LoadTrusted は同梱の default スコープに userPath の user 設定を重ね、repo 宣言の無い Config を返す。
// global rule と secret 定義だけを使う操作 (policy sync・secret setup) のため。merge の規則は Load と同じものを通る。
func LoadTrusted(userPath string) (Config, error) {
	user, err := ReadUserFile(userPath)
	if err != nil {
		return Config{}, err
	}
	return user.Trusted()
}

// UserFile は user 設定を、ファイルごとの検証を通して読んだもの。merge へはこの形でだけ渡す (Config が検証済みであるため)。
type UserFile struct {
	decl Declaration
}

// RepoFile は repo 宣言を、スコープ制限を含むファイルごとの検証を通して読んだもの。
type RepoFile struct {
	decl Declaration
}

// ReadUserFile は path の user 設定を読む。ファイルが無ければ宣言の無い user 設定。
func ReadUserFile(path string) (UserFile, error) {
	decl, err := parseFileIfExists(ScopeUser, path)
	return UserFile{decl: decl}, err
}

// ReadRepoFile は path の repo 宣言を読む。ファイルが無ければ宣言の無い repo 宣言。
// user 設定とは独立に読めるので、user 設定の誤りがあっても repo 宣言の誤りを確かめられる (sbxr doctor)。
func ReadRepoFile(path string) (RepoFile, error) {
	decl, err := parseFileIfExists(ScopeRepo, path)
	return RepoFile{decl: decl}, err
}

// Trusted は同梱の default に user 設定を重ね、repo 宣言の無い Config を返す。
func (u UserFile) Trusted() (Config, error) {
	return mergeOverDefault(u.decl, Declaration{})
}

// With は同梱の default に user 設定と repo 宣言を重ねる。
func (u UserFile) With(repo RepoFile) (Config, error) {
	return mergeOverDefault(u.decl, repo.decl)
}

// SandboxEgress は repo 宣言の egress を、group の中身が揃っていることを確かめてから宛先にする。
// merge と同じ規則で、user 設定に依らずに決まる (repo の egress は default と user とは別に重ねる)。
func (r RepoFile) SandboxEgress() ([]string, error) {
	return sandboxEgress(r.decl)
}

func mergeOverDefault(userDecl, repoDecl Declaration) (Config, error) {
	defaultDecl, err := Parse(ScopeDefault, "同梱の default 宣言", assets.DefaultDeclaration)
	if err != nil {
		return Config{}, err
	}
	return merge(defaultDecl, userDecl, repoDecl)
}

func parseFileIfExists(scope Scope, path string) (Declaration, error) {
	if path == "" {
		return Declaration{}, fmt.Errorf("%s スコープの宣言ファイルの path が空", scope)
	}
	// 無いのが path そのものなら宣言が無いスコープ。リンク先が消えた symlink などは読めない error として止める
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return Declaration{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Declaration{}, fmt.Errorf("%s を読めない: %w", path, err)
	}
	return Parse(scope, path, data)
}
