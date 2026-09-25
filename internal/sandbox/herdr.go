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

	"go.yaml.in/yaml/v3"

	"github.com/swat9013/sbxr/internal/herdr"
	"github.com/swat9013/sbxr/internal/runtime"
)

// Hosts は sbxr が扱う外部: sandbox VM の実行基盤と host の herdr。
type Hosts struct {
	Runtime runtime.Runtime
	Herdr   herdr.Client
}

// kitStartupLog は sbx が VM 内に kit startup の経過を書く log (VM の /etc/durable-startup.d/run.sh が決める)。
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
	// Recovery は利用者が手で登録し直す手順。
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
	return requireHerdrOnHost(client)
}

// RequireHerdrFor は、herdr 連携を有効にして作った sandbox VM なら host に herdr があることを確かめる。
// destroy が確認の前に呼ぶ。
func RequireHerdrFor(places Places, name string, client herdr.Client) error {
	enabled, err := herdrEnabled(places, name)
	if err != nil || !enabled {
		return err
	}
	return requireHerdrOnHost(client)
}

func requireHerdrOnHost(client herdr.Client) error {
	if err := client.Available(); err != nil {
		return fmt.Errorf("herdr 連携が有効だが、%w (PATH に herdr を入れる。作成前なら user 設定で herdr.enabled: false にする)", err)
	}
	return nil
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

// stopHerdrServer は VM 内の herdr server を止めるコマンド。
const stopHerdrServer = "pkill -x herdr"

// registerHerdrMachine は host の herdr に <name>.sbx を登録する。
// kit が起動した server を止めてから登録する (動いたままだと herdr machine add が
// "remote server is not ready for saved machines" で失敗する。ADR 0007 の実測)。
// 同じ宛先の登録が残っていたら (前の VM の解除の失敗など)、この回に作っていない登録には触れずに止める。
func registerHerdrMachine(ctx context.Context, hosts Hosts, name string, progress io.Writer) error {
	target := herdr.Target(name)
	recovery := fmt.Sprintf("sbx exec %s -- %s; %s", name, stopHerdrServer, herdr.AddCommand(target, name))
	machines, err := hosts.Herdr.List(ctx)
	if err != nil {
		return &HerdrMachineError{Err: err, Recovery: recovery}
	}
	if stale, ok := herdr.Find(machines, target); ok {
		return &HerdrMachineError{
			Err:      fmt.Errorf("%s の登録 %s が既にある (前の sandbox VM の残りなら解除してから登録し直す)", target, stale.ID),
			Recovery: herdr.RemoveCommand(stale.ID) + "; " + recovery,
		}
	}
	// server が動いていなければ pkill は 1 で終わるので、それは成功として扱う
	stop := runtime.SandboxCommand{Args: []string{"sh", "-c", stopHerdrServer + " || [ $? -eq 1 ]"}}
	if _, err := hosts.Runtime.ExecInSandbox(ctx, name, stop); err != nil {
		return &HerdrMachineError{Err: fmt.Errorf("VM 内の herdr server を止められない: %w", err), Recovery: recovery}
	}
	if err := hosts.Herdr.Add(ctx, target, name); err != nil {
		return &HerdrMachineError{Err: err, Recovery: recovery}
	}
	logf(progress, "herdr: %s を登録した\n", target)
	return nil
}

// herdrEnabled は sandbox VM を herdr 連携を有効にして作ったかを、状態ディレクトリの env 定義に herdr の kit があるかで返す。
// env 定義は作成の最初に書くので、作成が途中で止まった VM でも読める。
func herdrEnabled(places Places, name string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(places.StateDir(name), envFile))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var env envDefinition
	if err := yaml.Unmarshal(data, &env); err != nil {
		return false, fmt.Errorf("状態ディレクトリの %s を読めない: %w", envFile, err)
	}
	herdrSource := embeddedKit{name: herdrKit}.source()
	for _, kit := range env.Kits {
		if kit.Source == herdrSource {
			return true, nil
		}
	}
	return false, nil
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
		return fmt.Errorf("herdr machine %s を解除できない (%s で解除する): %w", machine.Target, herdr.RemoveCommand(machine.ID), err)
	}
	return nil
}

func findHerdrMachine(ctx context.Context, client herdr.Client, name string) (herdr.Machine, bool, error) {
	machines, err := client.List(ctx)
	if err != nil {
		return herdr.Machine{}, false, err
	}
	machine, found := herdr.Find(machines, herdr.Target(name))
	return machine, found, nil
}

// Stop は sandbox VM を止める。herdr 連携を有効にして作った VM は、先に herdr machine を無効にする
// (有効なままだと herdr が繋ぎ直して VM が起動し直す。ADR 0007)。無効にしたら、有効に戻すコマンドを出す。
// host に herdr が無ければ、VM に触れずに error で止める。
func Stop(ctx context.Context, hosts Hosts, places Places, name string, progress io.Writer) error {
	enabled, err := herdrEnabled(places, name)
	if err != nil {
		return err
	}
	if !enabled {
		return hosts.Runtime.StopSandbox(ctx, name)
	}
	if err := requireHerdrOnHost(hosts.Herdr); err != nil {
		return err
	}
	machine, found, err := findHerdrMachine(ctx, hosts.Herdr, name)
	if err != nil {
		return fmt.Errorf("herdr machine を無効にできないので止めない: %w", err)
	}
	if !found {
		logf(progress, "herdr: 警告 %s の登録が無い (無効にするものが無いので、そのまま止める)\n", herdr.Target(name))
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
