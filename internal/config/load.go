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
	defaultDecl, err := Parse(ScopeDefault, "同梱の default 宣言", assets.DefaultDeclaration)
	if err != nil {
		return Config{}, err
	}
	userDecl, err := parseFileIfExists(ScopeUser, userPath)
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
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Declaration{}, nil
	}
	if err != nil {
		return Declaration{}, fmt.Errorf("%s を読めない: %w", path, err)
	}
	return Parse(scope, path, data)
}
