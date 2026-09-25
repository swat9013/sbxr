package secret

import (
	"bufio"
	"bytes"
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
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Values{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("secret ファイル %s を読めない: %w", path, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		return nil, fmt.Errorf("secret ファイル %s の mode が %04o (0600 にする: chmod 600 %s)", path, perm, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("secret ファイル %s を読めない: %w", path, err)
	}
	values := Values{}
	for number, line := range lines(data) {
		key, value, ok, err := parseLine(line)
		if err != nil {
			return nil, fmt.Errorf("secret ファイル %s の %d 行目: %w", path, number+1, err)
		}
		if ok {
			values[key] = value
		}
	}
	return values, nil
}

func lines(data []byte) []string {
	var out []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		out = append(out, scanner.Text())
	}
	return out
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

// WriteValue は secret ファイルの key に value を書く。既にある key の行は置き換え、他の行はそのまま残す。
// ファイルとディレクトリが無ければ作り、ファイルは mode 0600 で置き換える。
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
	if _, err := ReadFile(path); err != nil {
		return err // mode の誤りや壊れた行は、書き足す前に直してもらう
	}
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("secret ファイル %s を読めない: %w", path, err)
	}

	newLine := key + "=" + value
	var out []string
	replaced := false
	for _, line := range lines(existing) {
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
	return writeFileAtomically(path, []byte(strings.Join(out, "\n")+"\n"))
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
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("secret ファイル %s を置き換えられない: %w", path, err)
	}
	return nil
}
