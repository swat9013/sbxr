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
	defaultDecl, userDecl, err := parseTrustedScopes(userPath)
	if err != nil {
		return Config{}, err
	}
	repoDecl, err := parseFileIfExists(ScopeRepo, repoPath)
	if err != nil {
		return Config{}, err
	}
	return Merge(defaultDecl, userDecl, repoDecl)
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

// LoadTrusted は同梱の default スコープに userPath の user 設定を重ね、repo 宣言の無い Config を返す。
// global rule と secret 定義だけを使う操作 (policy sync・secret setup) のため。merge の規則は Load と同じものを通る。
func LoadTrusted(userPath string) (Config, error) {
	defaultDecl, userDecl, err := parseTrustedScopes(userPath)
	if err != nil {
		return Config{}, err
	}
	return Merge(defaultDecl, userDecl, Declaration{})
}

// parseTrustedScopes は同梱の default 宣言と userPath の user 設定を読む。
func parseTrustedScopes(userPath string) (defaultDecl, userDecl Declaration, err error) {
	defaultDecl, err = Parse(ScopeDefault, "同梱の default 宣言", assets.DefaultDeclaration)
	if err != nil {
		return Declaration{}, Declaration{}, err
	}
	userDecl, err = parseFileIfExists(ScopeUser, userPath)
	if err != nil {
		return Declaration{}, Declaration{}, err
	}
	return defaultDecl, userDecl, nil
}
