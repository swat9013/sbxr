// Package runtimetest は runtime.Runtime の adapter が満たす契約 test を置く。
// Sbx adapter (sbx stub の上) と in-memory adapter の両方に同じ契約を流し、2 つの adapter の食い違いを防ぐ (ADR 0005)。
package runtimetest

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime"
)

// Harness は契約 test に渡す 1 つの adapter と、interface からは見えない状態の観測口。
type Harness struct {
	Runtime runtime.Runtime
	// SandboxSecrets は sandbox VM に置かれた sandbox スコープの secret の数。
	SandboxSecrets func(sandbox string) int
	// SandboxRules は sandbox VM に置かれた sandbox スコープ rule の宛先。
	SandboxRules func(sandbox string) []string
}

// Contract は adapter が実行基盤として満たす振る舞いを確かめる。newHarness は test ごとに空の実行基盤を返す。
// 振る舞いは sbx の実測に基づく (ADR 0006)。止まった VM への VM 内の操作は契約に入れない
// (実 sbx の exec は止まった VM を起こし直すが、2 つの test 用の実行基盤は稼働中の VM にしか届かない)。
func Contract(t *testing.T, newHarness func(t *testing.T) Harness) {
	ctx := context.Background()
	const boot = "/home/agent/.config/sbxr/boot.sh"

	t.Run("無い VM は absent", func(t *testing.T) {
		h := newHarness(t)

		assertStatus(t, h, "app", runtime.SandboxAbsent)
	})

	t.Run("作った VM は running", func(t *testing.T) {
		h := newHarness(t)
		dir := define(t, h, spec("app"))

		must(t, h.Runtime.CreateSandbox(ctx, dir, spec("app")))

		assertStatus(t, h, "app", runtime.SandboxRunning)
	})

	t.Run("定義の無い状態ディレクトリからは作れず、secret も置かない", func(t *testing.T) {
		h := newHarness(t)
		withSecret := spec("app")
		withSecret.Secrets = []runtime.SandboxSecret{{Service: "github", Value: "v"}}

		err := h.Runtime.CreateSandbox(ctx, t.TempDir(), withSecret)

		if err == nil || h.SandboxSecrets("app") != 0 {
			t.Errorf("CreateSandbox without a definition = %v, secrets = %d, want an error and no secret", err, h.SandboxSecrets("app"))
		}
	})

	t.Run("定義と名前の違う作る内容からは作れず、secret も置かない", func(t *testing.T) {
		h := newHarness(t)
		dir := define(t, h, spec("app"))
		other := spec("other")
		other.Secrets = []runtime.SandboxSecret{{Service: "github", Value: "v"}}

		err := h.Runtime.CreateSandbox(ctx, dir, other)

		if err == nil || h.SandboxSecrets("other") != 0 {
			t.Errorf("CreateSandbox with another name = %v, secrets = %d, want an error and no secret", err, h.SandboxSecrets("other"))
		}
		assertStatus(t, h, "other", runtime.SandboxAbsent)
	})

	t.Run("止めた VM は stopped", func(t *testing.T) {
		h := newHarness(t)
		create(t, h, spec("app"))

		must(t, h.Runtime.StopSandbox(ctx, "app"))

		assertStatus(t, h, "app", runtime.SandboxStopped)
	})

	t.Run("撤去した VM は absent", func(t *testing.T) {
		h := newHarness(t)
		dir := create(t, h, spec("app"))

		must(t, h.Runtime.RemoveEnvironment(ctx, dir))

		assertStatus(t, h, "app", runtime.SandboxAbsent)
	})

	t.Run("作る内容の secret は作った VM に置かれる", func(t *testing.T) {
		h := newHarness(t)
		withSecret := spec("app")
		withSecret.Secrets = []runtime.SandboxSecret{{Service: "github", Value: "v"}}

		create(t, h, withSecret)

		if got := h.SandboxSecrets("app"); got != 1 {
			t.Errorf("sandbox secrets = %d, want the secret of the spec", got)
		}
	})

	t.Run("作る内容の sandbox スコープ rule は作った VM に置かれる", func(t *testing.T) {
		h := newHarness(t)
		withRule := spec("app")
		withRule.EgressRules = []string{"api.example.com:443"}

		create(t, h, withRule)

		if got := h.SandboxRules("app"); !slices.Equal(got, []string{"api.example.com:443"}) {
			t.Errorf("sandbox rules = %v, want the rule of the spec", got)
		}
	})

	t.Run("撤去すると sandbox スコープの secret も消える", func(t *testing.T) {
		h := newHarness(t)
		withSecret := spec("app")
		withSecret.Secrets = []runtime.SandboxSecret{{Service: "github", Value: "v"}}
		dir := create(t, h, withSecret)

		must(t, h.Runtime.RemoveEnvironment(ctx, dir))

		if got := h.SandboxSecrets("app"); got != 0 {
			t.Errorf("sandbox secrets = %d, want none after removing the env", got)
		}
	})

	t.Run("VM を作る前の定義だけの状態ディレクトリも撤去できる", func(t *testing.T) {
		h := newHarness(t)
		dir := define(t, h, spec("app"))

		must(t, h.Runtime.RemoveEnvironment(ctx, dir))
	})

	t.Run("herdr を導入する定義は、導入すると答える", func(t *testing.T) {
		h := newHarness(t)
		withHerdr := spec("app")
		withHerdr.Herdr = &runtime.HerdrInstall{Version: "v0.9.0"}
		dir := define(t, h, withHerdr)

		got, err := h.Runtime.DefinedWithHerdr(dir)

		if err != nil || !got {
			t.Errorf("DefinedWithHerdr = %v, %v, want true", got, err)
		}
	})

	t.Run("herdr を導入しない定義は、導入しないと答える", func(t *testing.T) {
		h := newHarness(t)
		dir := define(t, h, spec("app"))

		got, err := h.Runtime.DefinedWithHerdr(dir)

		if err != nil || got {
			t.Errorf("DefinedWithHerdr = %v, %v, want false", got, err)
		}
	})

	t.Run("撤去しても定義は残り、そこから作り直せる", func(t *testing.T) {
		h := newHarness(t)
		dir := create(t, h, spec("app"))
		must(t, h.Runtime.RemoveEnvironment(ctx, dir))

		must(t, h.Runtime.CreateSandbox(ctx, dir, spec("app")))

		assertStatus(t, h, "app", runtime.SandboxRunning)
	})

	t.Run("撤去しても herdr を導入する定義は読める", func(t *testing.T) {
		h := newHarness(t)
		withHerdr := spec("app")
		withHerdr.Herdr = &runtime.HerdrInstall{Version: "v0.9.0"}
		dir := define(t, h, withHerdr)
		must(t, h.Runtime.RemoveEnvironment(ctx, dir))

		got, err := h.Runtime.DefinedWithHerdr(dir)

		if err != nil || !got {
			t.Errorf("DefinedWithHerdr after removing = %v, %v, want the definition kept", got, err)
		}
	})

	t.Run("消した状態ディレクトリの定義は無い", func(t *testing.T) {
		h := newHarness(t)
		withHerdr := spec("app")
		withHerdr.Herdr = &runtime.HerdrInstall{Version: "v0.9.0"}
		dir := define(t, h, withHerdr)
		must(t, os.RemoveAll(dir))

		got, err := h.Runtime.DefinedWithHerdr(dir)

		if err != nil || got {
			t.Errorf("DefinedWithHerdr of a removed state dir = %v, %v, want false", got, err)
		}
		if err := h.Runtime.RemoveEnvironment(ctx, dir); err == nil {
			t.Errorf("RemoveEnvironment of a removed state dir = nil, want an error")
		}
	})

	t.Run("定義の無い状態ディレクトリは、herdr を導入しないと答える", func(t *testing.T) {
		h := newHarness(t)

		got, err := h.Runtime.DefinedWithHerdr(t.TempDir())

		if err != nil || got {
			t.Errorf("DefinedWithHerdr = %v, %v, want false", got, err)
		}
	})

	t.Run("無い VM は止められない", func(t *testing.T) {
		h := newHarness(t)

		if err := h.Runtime.StopSandbox(ctx, "app"); err == nil {
			t.Errorf("StopSandbox of an absent VM = nil, want an error")
		}
	})

	t.Run("VM に書いたファイルは読める", func(t *testing.T) {
		h := newHarness(t)
		create(t, h, spec("app"))
		must(t, h.Runtime.WriteSandboxFile(ctx, "app", boot, []byte("echo boot\n"), 0o755))

		data, err := h.Runtime.ReadSandboxFile(ctx, "app", boot)

		if err != nil || string(data) != "echo boot\n" {
			t.Errorf("ReadSandboxFile = %q, %v, want what was written", data, err)
		}
	})

	t.Run("VM に書いたファイルはある", func(t *testing.T) {
		h := newHarness(t)
		create(t, h, spec("app"))
		must(t, h.Runtime.WriteSandboxFile(ctx, "app", boot, []byte("echo boot\n"), runtime.KeepMode))

		found, err := h.Runtime.SandboxFileExists(ctx, "app", boot)

		if err != nil || !found {
			t.Errorf("SandboxFileExists = %v, %v, want true", found, err)
		}
	})

	t.Run("VM に書いたファイルの親ディレクトリはある", func(t *testing.T) {
		h := newHarness(t)
		create(t, h, spec("app"))
		must(t, h.Runtime.WriteSandboxFile(ctx, "app", boot, []byte("echo boot\n"), runtime.KeepMode))

		found, err := h.Runtime.SandboxFileExists(ctx, "app", filepath.Dir(boot))

		if err != nil || !found {
			t.Errorf("SandboxFileExists = %v, %v, want true", found, err)
		}
	})

	t.Run("VM に無いファイルは無いと答える", func(t *testing.T) {
		h := newHarness(t)
		create(t, h, spec("app"))

		found, err := h.Runtime.SandboxFileExists(ctx, "app", "/home/agent/missing")

		if err != nil || found {
			t.Errorf("SandboxFileExists = %v, %v, want false", found, err)
		}
	})

	t.Run("VM に無いファイルを読むと error", func(t *testing.T) {
		h := newHarness(t)
		create(t, h, spec("app"))

		if _, err := h.Runtime.ReadSandboxFile(ctx, "app", "/home/agent/missing"); err == nil {
			t.Errorf("ReadSandboxFile of a missing file = nil, want an error")
		}
	})

	t.Run("撤去して作り直した VM に前のファイルは無い", func(t *testing.T) {
		h := newHarness(t)
		dir := create(t, h, spec("app"))
		must(t, h.Runtime.WriteSandboxFile(ctx, "app", boot, []byte("echo boot\n"), 0o755))
		must(t, h.Runtime.RemoveEnvironment(ctx, dir))
		must(t, h.Runtime.DefineSandbox(dir, spec("app")))
		must(t, h.Runtime.CreateSandbox(ctx, dir, spec("app")))

		found, err := h.Runtime.SandboxFileExists(ctx, "app", boot)

		if err != nil || found {
			t.Errorf("SandboxFileExists = %v, %v, want the recreated VM to start empty", found, err)
		}
	})

	t.Run("ssh target は空でなく、sandbox VM ごとに違う", func(t *testing.T) {
		h := newHarness(t)

		app, api := h.Runtime.SSHTarget("app"), h.Runtime.SSHTarget("api")

		if app == "" || app == api {
			t.Errorf("SSHTarget = %q and %q, want distinct non-empty targets", app, api)
		}
	})

	t.Run("足した global rule は allow の rule として一覧に出る", func(t *testing.T) {
		h := newHarness(t)
		must(t, h.Runtime.AllowGlobalEgress(ctx, "github.com:443"))

		rules, err := h.Runtime.ListGlobalEgressRules(ctx)

		if err != nil || len(rules) != 1 || rules[0].Decision != runtime.DecisionAllow || !slices.Equal(rules[0].Resources, []string{"github.com:443"}) {
			t.Errorf("ListGlobalEgressRules = %+v, %v, want one allow rule for github.com:443", rules, err)
		}
	})

	t.Run("消した global rule は一覧から消える", func(t *testing.T) {
		h := newHarness(t)
		must(t, h.Runtime.AllowGlobalEgress(ctx, "github.com:443"))
		rules, err := h.Runtime.ListGlobalEgressRules(ctx)
		if err != nil || len(rules) != 1 {
			t.Fatalf("ListGlobalEgressRules = %+v, %v, want the added rule", rules, err)
		}

		must(t, h.Runtime.RemoveGlobalEgressRule(ctx, rules[0].ID))

		if rules, err := h.Runtime.ListGlobalEgressRules(ctx); err != nil || len(rules) != 0 {
			t.Errorf("ListGlobalEgressRules = %+v, %v, want none", rules, err)
		}
	})

	t.Run("無い global rule は消せない", func(t *testing.T) {
		h := newHarness(t)

		if err := h.Runtime.RemoveGlobalEgressRule(ctx, "missing"); err == nil {
			t.Errorf("RemoveGlobalEgressRule of a missing rule = nil, want an error")
		}
	})
}

// spec は name の sandbox VM の、herdr も secret も rule も持たない作る内容。
func spec(name string) runtime.SandboxSpec {
	return runtime.SandboxSpec{Name: name, Repo: "/src/" + name}
}

// define は新しい状態ディレクトリに spec を定義して返す。
func define(t *testing.T, h Harness, spec runtime.SandboxSpec) string {
	t.Helper()
	dir := t.TempDir()
	must(t, h.Runtime.DefineSandbox(dir, spec))
	return dir
}

// create は spec を定義して VM を作り、状態ディレクトリを返す。
func create(t *testing.T, h Harness, spec runtime.SandboxSpec) string {
	t.Helper()
	dir := define(t, h, spec)
	must(t, h.Runtime.CreateSandbox(context.Background(), dir, spec))
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
