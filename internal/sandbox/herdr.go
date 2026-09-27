package sandbox

import (
	"context"
	"fmt"
	"io"

	"github.com/swat9013/sbxr/internal/herdr"
	"github.com/swat9013/sbxr/internal/runtime"
)

// Hosts は sbxr が扱う外部: sandbox VM の実行基盤と host の herdr。
type Hosts struct {
	Runtime runtime.Runtime
	Herdr   herdr.Client
}

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
// herdr 連携の有無は、作成の最初の記録から読む (作成が途中で止まった VM でも読める)。
// destroy が確認の前に呼ぶ。
func RequireHerdrFor(hosts Hosts, places Places, name string) error {
	enabled, err := places.stateDir(name).herdrEnabled(hosts.Runtime)
	if err != nil || !enabled {
		return err
	}
	return requireHerdrOnHost(hosts.Herdr)
}

func requireHerdrOnHost(client herdr.Client) error {
	if err := client.Available(); err != nil {
		return fmt.Errorf("herdr 連携が有効だが、%w (PATH に herdr を入れる。作成前なら user 設定で herdr.enabled: false にする)", err)
	}
	return nil
}

// stopHerdrServer は VM 内の herdr server を止めるコマンド。
const stopHerdrServer = "pkill -x herdr"

// registerHerdrMachine は host の herdr に <name>.sbx を登録する。
// kit が起動した server を止めてから登録する (動いたままだと herdr machine add が
// "remote server is not ready for saved machines" で失敗する。ADR 0007 の実測)。
// 同じ宛先の登録が残っていたら (前の VM の解除の失敗など)、この回に作っていない登録には触れずに止める。
func registerHerdrMachine(ctx context.Context, hosts Hosts, name string, progress io.Writer) error {
	target := hosts.Runtime.SSHTarget(name)
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

// removeHerdrMachine は herdr 連携を有効にして作った VM の herdr machine を解除する。登録が無ければ何もしない。
func removeHerdrMachine(ctx context.Context, hosts Hosts, places Places, name string) error {
	enabled, err := places.stateDir(name).herdrEnabled(hosts.Runtime)
	if err != nil || !enabled {
		return err
	}
	machine, found, err := findHerdrMachine(ctx, hosts.Herdr, hosts.Runtime.SSHTarget(name))
	if err != nil || !found {
		return err
	}
	if err := hosts.Herdr.Remove(ctx, machine.ID); err != nil {
		return fmt.Errorf("herdr machine %s を解除できない (%s で解除する): %w", machine.Target, herdr.RemoveCommand(machine.ID), err)
	}
	return nil
}

func findHerdrMachine(ctx context.Context, client herdr.Client, target string) (herdr.Machine, bool, error) {
	machines, err := client.List(ctx)
	if err != nil {
		return herdr.Machine{}, false, err
	}
	machine, found := herdr.Find(machines, target)
	return machine, found, nil
}

// Stop は sandbox VM を止める。herdr 連携を有効にして作った VM は、先に herdr machine を無効にする
// (有効なままだと herdr が繋ぎ直して VM が起動し直す。ADR 0007)。無効にしたら、有効に戻すコマンドを出す。
// host に herdr が無ければ、VM に触れずに error で止める。
func Stop(ctx context.Context, hosts Hosts, places Places, name string, progress io.Writer) error {
	enabled, err := places.stateDir(name).herdrEnabled(hosts.Runtime)
	if err != nil {
		return err
	}
	if !enabled {
		return hosts.Runtime.StopSandbox(ctx, name)
	}
	if err := requireHerdrOnHost(hosts.Herdr); err != nil {
		return err
	}
	target := hosts.Runtime.SSHTarget(name)
	machine, found, err := findHerdrMachine(ctx, hosts.Herdr, target)
	if err != nil {
		return fmt.Errorf("herdr machine を無効にできないので止めない: %w", err)
	}
	if !found {
		logf(progress, "herdr: 警告 %s の登録が無い (無効にするものが無いので、そのまま止める)\n", target)
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
