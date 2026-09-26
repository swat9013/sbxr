// Package assets は CLI に同梱する資材 (default スコープの宣言と sbx の kit) を埋め込む。
package assets

import (
	"embed"
	"io/fs"
)

// DefaultDeclaration は default スコープの宣言 (schema v1)。user 設定と repo 宣言がこの上に重なる。
//
//go:embed default.yaml
var DefaultDeclaration []byte

//go:embed kits
var kits embed.FS

// Kits は sandbox VM に入れる sbx の kit。root 直下の各ディレクトリが 1 つの kit。
func Kits() fs.FS {
	sub, err := fs.Sub(kits, "kits")
	if err != nil {
		panic(err) // 埋め込みの path は build 時に決まるので、ここで失敗するなら build が壊れている
	}
	return sub
}
