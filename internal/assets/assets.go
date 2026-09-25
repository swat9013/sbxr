// Package assets は CLI に同梱する資材 (default スコープの宣言など) を埋め込む。
package assets

import _ "embed"

// DefaultDeclaration は default スコープの宣言 (schema v1)。user 設定と repo 宣言がこの上に重なる。
//
//go:embed default.yaml
var DefaultDeclaration []byte
