// Package shell は、文字列を POSIX shell のコマンド行へ 1 つの引数として置く形にする。
package shell

import "strings"

// Quote は s を単一引用符で囲み、中の単一引用符を閉じて escape して開き直す。
func Quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Join は args を 1 行のコマンドにする。shell が特別に扱う文字を含む引数 (と空の引数) だけを Quote で囲む。
func Join(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if arg == "" || strings.ContainsFunc(arg, needsQuote) {
			arg = Quote(arg)
		}
		quoted[i] = arg
	}
	return strings.Join(quoted, " ")
}

func needsQuote(r rune) bool {
	switch {
	case 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z', '0' <= r && r <= '9':
		return false
	}
	return !strings.ContainsRune("-_./:@%+=,", r)
}
