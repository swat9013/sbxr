// Package guide は、host の AI coding agent が sbxr の設定 (user 設定と repo 宣言) を書くための案内を持つ。
// 案内は binary に埋め込み、`sbxr guide` が出す。案内の key の表と yaml の例が宣言の型とずれないことは、
// internal/config の test が確かめる。
package guide

import _ "embed"

// Text は案内の全文 (Markdown)。
//
//go:embed guide.md
var Text string
