package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/swat9013/sbxr/internal/herdr"
	"github.com/swat9013/sbxr/internal/runtime"
)

// Hosts は sbxr が扱う外部: sandbox VM の実行基盤と host の herdr。
type Hosts struct {
	Runtime runtime.Runtime
	Herdr   herdr.Client
}

// kitStartupLog は sbx が VM 内に kit startup の経過を書く log。
const kitStartupLog = "/var/log/sbx-kit-startup.log"

// kitWait は kit startup の完了を待つ上限と間隔。sleep は test が差し替える。
// 上限は herdr の release の取得と server の起動を含む。sbx exec にかかる時間は数えないので、実際の待ちは上限より長くなりうる。
var kitWait = struct {
	budget, interval time.Duration
	sleep            func(time.Duration)
}{300 * time.Second, 2 * time.Second, time.Sleep}

// HerdrMachineError は herdr machine の登録に失敗したときの error。sandbox VM は作り終えている。
type HerdrMachineError struct {
	Err error
	// Recovery は利用者が手で登録し直すコマンド。
	Recovery string
}

func (e *HerdrMachineError) Error() string {
	return fmt.Sprintf("herdr machine を登録できない: %v", e.Err)
}
func (e *HerdrMachineError) Unwrap() error { return e.Err }

// RequireHerdr は herdr 連携が有効なら host に herdr があることを確かめる。無効なら herdr を呼ばない。
func (p Prepared) RequireHerdr(client herdr.Client) error {
	if p.Declaration.Herdr == nil {
		return nil
	}
	return client.Available()
}

// waitKitStartup は VM の起動時の kit startup が終わるまで待つ。失敗 (fail 行) か上限で error。
// kit startup の失敗は host からは見えないので、create 時はここで見る (boot の失敗と同じ扱い)。
func waitKitStartup(ctx context.Context, rt runtime.Runtime, name string) error {
	var readErr error
	for waited := time.Duration(0); waited < kitWait.budget; waited += kitWait.interval {
		if err := ctx.Err(); err != nil {
			return err
		}
		var out []byte
		out, readErr = rt.ExecInSandbox(ctx, name, runtime.SandboxCommand{Args: []string{"cat", kitStartupLog}}) // log は startup の途中まで無い
		switch startupOutcome(string(out)) {
		case startupComplete:
			return nil
		case startupFailed:
			return fmt.Errorf("VM の kit startup が失敗した (sbx exec %s -- cat %s で確かめる)", name, kitStartupLog)
		}
		kitWait.sleep(kitWait.interval)
	}
	if readErr != nil {
		return fmt.Errorf("VM の kit startup が %s の間に終わらない (最後の log の読み取り: %w)", kitWait.budget, readErr)
	}
	return fmt.Errorf("VM の kit startup が %s の間に終わらない (sbx exec %s -- tail %s で確かめる)", kitWait.budget, name, kitStartupLog)
}

type startup int

const (
	startupRunning startup = iota
	startupComplete
	startupFailed
)

// herdrKitFailure は kit sbxr-herdr が段の失敗を log に残す行の頭。kit は後段の boot を止めないよう、失敗しても 0 で終わる。
const herdrKitFailure = "sbxr-herdr: fail "

// startupOutcome は kit startup の log の最後の実行から、完了・失敗・実行中を読む。
// dispatcher の行の文面は sbx v0.45.1 の VM の /etc/durable-startup.d/run.sh に合わせている
// (段の失敗は "fail <script> exit=<N>" で、そこで止まる)。
func startupOutcome(log string) startup {
	if i := strings.LastIndex(log, "=== dispatcher run"); i >= 0 {
		log = log[i:]
	}
	for _, line := range strings.Split(log, "\n") {
		dispatcherFail := strings.HasPrefix(line, "fail /etc/durable-startup.d/") && strings.Contains(line, " exit=")
		if dispatcherFail || strings.HasPrefix(line, herdrKitFailure) {
			return startupFailed
		}
	}
	if strings.Contains(log, "=== dispatcher complete ===") {
		return startupComplete
	}
	return startupRunning
}

// registerHerdrMachine は host の herdr に <name>.sbx を登録する。同じ宛先の登録が残っていたら (前の VM の解除の失敗など)、
// 無効のまま残さないよう解除してから登録し直す。
// kit が起動した server を止めてから登録する (herdr machine add が server を登録用に起動し直す。旧実装の実測)。
func registerHerdrMachine(ctx context.Context, hosts Hosts, name string, progress io.Writer) error {
	target := herdr.Target(name)
	recovery := fmt.Sprintf("sbx exec %s -- pkill -x herdr; %s", name, herdr.AddCommand(target, name))
	fail := func(err error) error { return &HerdrMachineError{Err: err, Recovery: recovery} }
	machines, err := hosts.Herdr.List(ctx)
	if err != nil {
		return fail(err)
	}
	if stale, ok := herdr.Find(machines, target); ok {
		if err := hosts.Herdr.Remove(ctx, stale.ID); err != nil {
			return fail(fmt.Errorf("残っていた登録 %s を解除できない: %w", stale.ID, err))
		}
		logf(progress, "herdr: 残っていた %s の登録 (%s) を解除した\n", target, stale.ID)
	}
	// pkill は server が動いていなければ 0 以外で終わるので、失敗を問わない
	_, _ = hosts.Runtime.ExecInSandbox(ctx, name, runtime.SandboxCommand{Args: []string{"pkill", "-x", "herdr"}})
	if err := hosts.Herdr.Add(ctx, target, name); err != nil {
		return fail(err)
	}
	logf(progress, "herdr: %s を登録した\n", target)
	return nil
}

// herdrEnabled は sandbox VM を herdr 連携を有効にして作ったかを、状態ディレクトリの印で返す。
func herdrEnabled(places Places, name string) (bool, error) {
	_, err := os.Stat(filepath.Join(places.StateDir(name), herdrFile))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// removeHerdrMachine は herdr 連携を有効にして作った VM の herdr machine を解除する。登録が無ければ何もしない。
func removeHerdrMachine(ctx context.Context, hosts Hosts, places Places, name string) error {
	enabled, err := herdrEnabled(places, name)
	if err != nil || !enabled {
		return err
	}
	machine, found, err := findHerdrMachine(ctx, hosts.Herdr, name)
	if err != nil || !found {
		return err
	}
	if err := hosts.Herdr.Remove(ctx, machine.ID); err != nil {
		return fmt.Errorf("herdr machine %s を解除できない (herdr machine remove %s で解除する): %w", machine.Target, machine.ID, err)
	}
	return nil
}

func findHerdrMachine(ctx context.Context, client herdr.Client, name string) (herdr.Machine, bool, error) {
	if err := client.Available(); err != nil {
		return herdr.Machine{}, false, err
	}
	machines, err := client.List(ctx)
	if err != nil {
		return herdr.Machine{}, false, err
	}
	machine, found := herdr.Find(machines, herdr.Target(name))
	return machine, found, nil
}

// Stop は sandbox VM を止める。herdr 連携を有効にして作った VM は、先に herdr machine を無効にする
// (有効なままだと herdr が繋ぎ直して VM が起動し直す。ADR 0007)。無効にしたら、有効に戻すコマンドを出す。
func Stop(ctx context.Context, hosts Hosts, places Places, name string, progress io.Writer) error {
	enabled, err := herdrEnabled(places, name)
	if err != nil {
		return err
	}
	if !enabled {
		return hosts.Runtime.StopSandbox(ctx, name)
	}
	if err := hosts.Herdr.Available(); err != nil { // 繋ぎ直す herdr が host に無いので、無効にせず止めてよい
		logf(progress, "herdr: 警告 host に herdr が無いので machine を無効にせずに止める: %v\n", err)
		return hosts.Runtime.StopSandbox(ctx, name)
	}
	machine, found, err := findHerdrMachine(ctx, hosts.Herdr, name)
	if err != nil {
		return fmt.Errorf("herdr machine を無効にできないので止めない: %w", err)
	}
	if !found {
		return hosts.Runtime.StopSandbox(ctx, name)
	}
	if err := hosts.Herdr.Disable(ctx, machine.ID); err != nil {
		return fmt.Errorf("herdr machine %s を無効にできないので止めない: %w", machine.Target, err)
	}
	if err := hosts.Runtime.StopSandbox(ctx, name); err != nil {
		// VM は動いたままなので、herdr から見失わないよう有効に戻す
		if enableErr := hosts.Herdr.Enable(ctx, machine.ID); enableErr != nil {
			return fmt.Errorf("%w (無効にした herdr machine も有効に戻せない: %s で戻す: %v)", err, herdr.EnableCommand(machine.ID), enableErr)
		}
		return err
	}
	logf(progress, "herdr: %s を無効にした (起動し直したら %s で有効に戻す)\n", machine.Target, herdr.EnableCommand(machine.ID))
	return nil
}
