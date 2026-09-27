// Package runtimetest は runtime.Runtime の adapter が満たす契約 test を置く。
// Sbx adapter (sbx stub の上) と in-memory adapter の両方に同じ契約を流し、2 つの adapter の食い違いを防ぐ (ADR 0005)。
package runtimetest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime"
)

// Harness は契約 test に渡す 1 つの adapter と、interface からは見えない状態の観測口。
type Harness struct {
	Runtime runtime.Runtime
	// SandboxSecrets は sandbox VM に置かれた sandbox スコープの secret の数。
	SandboxSecrets func(sandbox string) int
}

// Contract は adapter が実行基盤として満たす振る舞いを確かめる。newHarness は test ごとに空の実行基盤を返す。
// 振る舞いは sbx の実測に基づく (ADR 0006)。
func Contract(t *testing.T, newHarness func(t *testing.T) Harness) {
	ctx := context.Background()

	t.Run("VM は作成で稼働し、止めると止まり、撤去すると無くなる", func(t *testing.T) {
		h := newHarness(t)
		env := EnvDir(t, "app")
		assertStatus(t, h, "app", runtime.SandboxAbsent)

		must(t, h.Runtime.CreateEnvironment(ctx, env))
		assertStatus(t, h, "app", runtime.SandboxRunning)
		must(t, h.Runtime.StopSandbox(ctx, "app"))
		assertStatus(t, h, "app", runtime.SandboxStopped)
		must(t, h.Runtime.RemoveEnvironment(ctx, env))
		assertStatus(t, h, "app", runtime.SandboxAbsent)
	})

	t.Run("sandbox スコープの secret は VM の作成前に置け、作成後も残る", func(t *testing.T) {
		h := newHarness(t)

		must(t, h.Runtime.SetSandboxSecret(ctx, "app", runtime.SandboxSecret{Service: "github", Value: "v"}))
		must(t, h.Runtime.CreateEnvironment(ctx, EnvDir(t, "app")))

		if got := h.SandboxSecrets("app"); got != 1 {
			t.Errorf("sandbox secrets = %d, want the secret placed before the VM to stay", got)
		}
	})

	t.Run("VM が無くても env 定義の撤去は成功し、sandbox スコープの secret を消す", func(t *testing.T) {
		h := newHarness(t)
		must(t, h.Runtime.SetSandboxSecret(ctx, "app", runtime.SandboxSecret{Service: "github", Value: "v"}))

		must(t, h.Runtime.RemoveEnvironment(ctx, EnvDir(t, "app")))

		if got := h.SandboxSecrets("app"); got != 0 {
			t.Errorf("sandbox secrets = %d, want none after removing the env", got)
		}
	})

	t.Run("sandbox スコープ rule は VM の作成前には置けない", func(t *testing.T) {
		h := newHarness(t)

		if err := h.Runtime.AllowSandboxEgress(ctx, "app", "api.example.com:443"); err == nil {
			t.Errorf("AllowSandboxEgress before the VM = nil, want an error")
		}
	})

	t.Run("sandbox スコープ rule は VM の作成後に置ける", func(t *testing.T) {
		h := newHarness(t)
		must(t, h.Runtime.CreateEnvironment(ctx, EnvDir(t, "app")))

		must(t, h.Runtime.AllowSandboxEgress(ctx, "app", "api.example.com:443"))
	})

	t.Run("無い VM は止められない", func(t *testing.T) {
		h := newHarness(t)

		if err := h.Runtime.StopSandbox(ctx, "app"); err == nil {
			t.Errorf("StopSandbox of an absent VM = nil, want an error")
		}
	})

	t.Run("止まった VM ではコマンドを走らせられない", func(t *testing.T) {
		h := newHarness(t)
		must(t, h.Runtime.CreateEnvironment(ctx, EnvDir(t, "app")))
		must(t, h.Runtime.StopSandbox(ctx, "app"))

		if _, err := h.Runtime.ExecInSandbox(ctx, "app", runtime.SandboxCommand{Args: []string{"true"}}); err == nil {
			t.Errorf("ExecInSandbox on a stopped VM = nil, want an error")
		}
	})

	t.Run("VM に書いたファイルは読め、あると答える", func(t *testing.T) {
		h := newHarness(t)
		must(t, h.Runtime.CreateEnvironment(ctx, EnvDir(t, "app")))

		must(t, h.Runtime.WriteSandboxFile(ctx, "app", "/home/agent/.config/sbxr/boot.sh", []byte("echo boot\n"), 0o755))
		data, err := h.Runtime.ReadSandboxFile(ctx, "app", "/home/agent/.config/sbxr/boot.sh")
		found, existsErr := h.Runtime.SandboxFileExists(ctx, "app", "/home/agent/.config/sbxr/boot.sh")

		if err != nil || string(data) != "echo boot\n" {
			t.Errorf("ReadSandboxFile = %q, %v, want what was written", data, err)
		}
		if existsErr != nil || !found {
			t.Errorf("SandboxFileExists = %v, %v, want true", found, existsErr)
		}
	})

	t.Run("VM に無いファイルは無いと答え、読むと error", func(t *testing.T) {
		h := newHarness(t)
		must(t, h.Runtime.CreateEnvironment(ctx, EnvDir(t, "app")))

		found, err := h.Runtime.SandboxFileExists(ctx, "app", "/home/agent/missing")
		_, readErr := h.Runtime.ReadSandboxFile(ctx, "app", "/home/agent/missing")

		if err != nil || found {
			t.Errorf("SandboxFileExists = %v, %v, want false", found, err)
		}
		if readErr == nil {
			t.Errorf("ReadSandboxFile of a missing file = nil, want an error")
		}
	})

	t.Run("ssh target は空でなく、sandbox VM ごとに違う", func(t *testing.T) {
		h := newHarness(t)

		app, api := h.Runtime.SSHTarget("app"), h.Runtime.SSHTarget("api")

		if app == "" || app == api {
			t.Errorf("SSHTarget = %q and %q, want distinct non-empty targets", app, api)
		}
	})
}

// EnvDir は name の sandbox VM を指す env 定義を置いたディレクトリを返す。
func EnvDir(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sbxenv.yaml"), []byte("name: "+name+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertStatus(t *testing.T, h Harness, sandbox string, want runtime.SandboxStatus) {
	t.Helper()
	if got, err := h.Runtime.SandboxStatus(context.Background(), sandbox); err != nil || got != want {
		t.Errorf("SandboxStatus(%s) = %v, %v, want %v", sandbox, got, err, want)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
