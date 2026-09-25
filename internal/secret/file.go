package secret

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Values は secret ファイルの key と値。
type Values map[string]string

// keyPattern は secret ファイルのキーと VM の環境変数名に使える名前。
var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ReadFile は secret ファイル (dotenv) を読む。mode が 0600 でなければ拒否する (ADR 0002)。
// ファイルが無ければ値が無いものとして扱う。
func ReadFile(path string) (Values, error) {
	_, values, err := readSecretFile(path)
	return values, err
}

// readSecretFile は secret ファイルの行と、それを読んだ値を返す。mode と各行の書式をここで検査する。
func readSecretFile(path string) ([]string, Values, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, Values{}, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("secret ファイル %s を読めない: %w", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		return nil, nil, fmt.Errorf("secret ファイル %s の mode が %04o (0600 にする: chmod 600 %s)", path, perm, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("secret ファイル %s を読めない: %w", path, err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(data) == 0 {
		lines = nil
	}
	values := Values{}
	for number, line := range lines {
		key, value, ok, err := parseLine(line)
		if err != nil {
			return nil, nil, fmt.Errorf("secret ファイル %s の %d 行目: %w", path, number+1, err)
		}
		if ok {
			values[key] = value
		}
	}
	return lines, values, nil
}

// parseLine は KEY=VALUE の 1 行を読む。空行と # で始まる行は ok=false。
// 値を同じ引用符 (" か ') で囲んでいれば外す。値の中の値展開やエスケープは解釈しない。
func parseLine(line string) (key, value string, ok bool, err error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false, nil
	}
	key, value, found := strings.Cut(trimmed, "=")
	if !found || !keyPattern.MatchString(key) {
		return "", "", false, errors.New("KEY=VALUE の形でない (値は表示しない)")
	}
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	return key, value, true, nil
}

// WriteValue は secret ファイルの key に value を書く。既にある key の行は 1 行に置き換え、他の行はそのまま残す。
// ファイルとディレクトリが無ければ作り、ファイルは mode 0600 で置き換える。path が symlink ならリンク先を書き換える。
// 既存のファイルの mode や書式が誤っていれば、書く前に止める。
func WriteValue(path, key, value string) error {
	if !keyPattern.MatchString(key) {
		return fmt.Errorf("secret ファイルのキー %q は英数字と _ だけで書く", key)
	}
	if strings.ContainsAny(value, "\r\n") {
		return errors.New("値に改行を含められない")
	}
	if value == "" {
		return errors.New("値が空")
	}
	target, err := resolveSymlink(path)
	if err != nil {
		return err
	}
	existing, _, err := readSecretFile(target)
	if err != nil {
		return err
	}
	newLine := key + "=" + value
	var out []string
	replaced := false
	for _, line := range existing {
		if lineKey, _, ok, _ := parseLine(line); ok && lineKey == key {
			if !replaced {
				out = append(out, newLine)
			}
			replaced = true
			continue
		}
		out = append(out, line)
	}
	if !replaced {
		out = append(out, newLine)
	}
	return writeFileAtomically(target, []byte(strings.Join(out, "\n")+"\n"))
}

// resolveSymlink は path が symlink ならリンク先を返す。rename で置き換えると symlink が通常ファイルに変わるため。
func resolveSymlink(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if errors.Is(err, fs.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", fmt.Errorf("secret ファイル %s のリンク先を解決できない: %w", path, err)
	}
	return resolved, nil
}

// writeFileAtomically は同じディレクトリの一時ファイルに mode 0600 で書いてから rename で置き換える。
// 書きかけのファイルや、一瞬でも 0600 より広い mode のファイルを残さないため。
func writeFileAtomically(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("secret ファイルの置き場 %s を作れない: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".secrets-*.env")
	if err != nil {
		return fmt.Errorf("secret ファイルの一時ファイルを作れない: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // rename 済みなら消す対象は無い
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secret ファイルの一時ファイル %s の mode を 0600 にできない: %w", tmp.Name(), err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secret ファイルの一時ファイル %s に書けない: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("secret ファイルの一時ファイル %s に書けない: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("secret ファイル %s を置き換えられない: %w", path, err)
	}
	return nil
}
