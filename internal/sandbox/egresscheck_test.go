package sandbox

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

// fakeCurl は -o の file へ body を書き、-w の代わりに状態コードを出して exit code で終える curl を PATH に置く。
func fakeCurl(t *testing.T, body, code string, exitCode int) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift ;;
  esac
  shift
done
printf '%s' "$FAKE_BODY" > "$out"
if [ "$FAKE_EXIT" -ne 0 ]; then echo "curl: (6) Could not resolve host" >&2; fi
printf '%s' "$FAKE_CODE"
exit "$FAKE_EXIT"
`
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_BODY", body)
	t.Setenv("FAKE_CODE", code)
	t.Setenv("FAKE_EXIT", strconv.Itoa(exitCode))
}

func TestProbeScriptPrintsTheCurlExitCodeTheStatusAndTheBodyHead(t *testing.T) {
	for _, tt := range []struct {
		name, body, code string
		exitCode         int
		want             string
	}{
		{"proxy の拒否応答", "Blocked by network policy: domain example.com:443\n", "403", 0, "0 403\nBlocked by network policy: domain example.com:443\n"},
		{"curl の失敗は error を出す", "", "000", 6, "6 000\ncurl: (6) Could not resolve host\n"},
		{"body は先頭 1024 byte だけ", strings.Repeat("x", 2000), "200", 0, "0 200\n" + strings.Repeat("x", 1024)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeCurl(t, tt.body, tt.code, tt.exitCode)

			out, err := exec.Command("sh", "-c", probeScript, "sh", "https://example.com/").Output()

			if err != nil || string(out) != tt.want {
				t.Errorf("probe script = %q, %v, want %q", out, err, tt.want)
			}
		})
	}
}

func TestCheckEgressSkipsTheSideWithoutAProbeTarget(t *testing.T) {
	for _, tt := range []struct {
		name       string
		global     []string
		http       map[string]sbxstub.HTTPResult
		wantProbes []string
		wantSkip   string
	}{
		{"glob を含まず 443 を通す許可先が無い", []string{"**.github.com:443", "ports.ubuntu.com:80"}, nil, []string{"probe https://example.com/"}, "許可先に届くかの確認を省いた"},
		{"許可外の候補がすべて許可されている", []string{"example.com:443", "example.net:443", "example.org:443"}, map[string]sbxstub.HTTPResult{"example.com": {Code: "200"}}, []string{"probe https://example.com/"}, "許可外に届かないかの確認を省いた"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fake := &sbxstub.FakeVM{HTTP: tt.http}
			var progress bytes.Buffer

			err := checkEgress(context.Background(), runningVM(t, fake), Prepared{GlobalEgress: tt.global}, &progress)

			if err != nil {
				t.Errorf("checkEgress() error = %v, want the remaining side to pass", err)
			}
			if !slices.Equal(fake.Events, tt.wantProbes) {
				t.Errorf("VM events = %q, want %q", fake.Events, tt.wantProbes)
			}
			if !strings.Contains(progress.String(), tt.wantSkip) {
				t.Errorf("progress = %q, want %q", progress.String(), tt.wantSkip)
			}
		})
	}
}
