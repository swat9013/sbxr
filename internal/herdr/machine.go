package herdr

import (
	"context"
	"fmt"
	"io"

	"github.com/swat9013/sbxr/internal/runtime"
)

// VM は herdr machine が sandbox VM に求めるもの: host から繋ぐ ssh の宛先と、VM 内のコマンドの実行。
type VM interface {
	SSHTarget(sandbox string) string
	ExecInSandbox(ctx context.Context, sandbox string, command runtime.SandboxCommand) ([]byte, error)
}

// Machines は sandbox VM ごとの herdr machine を扱う: host に herdr があるかの確認・登録・停止前の無効化・解除 (ADR 0007)。
// 残った登録の扱いと、失敗したときの復旧手順の文面はここに置く。
type Machines struct {
	Client Client
	VM     VM
}

// RequireOnHost は host に herdr があることを確かめる。無ければ、先へ進む手順を添えた error。
func (m Machines) RequireOnHost() error {
	if err := m.Client.Available(); err != nil {
		return fmt.Errorf("herdr 連携が有効だが、%w (PATH に herdr を入れる。作成前なら user 設定で herdr.enabled: false にする)", err)
	}
	return nil
}

// RegistrationError は herdr machine を登録できなかったときの error。sandbox VM は作り終えている。
type RegistrationError struct {
	Err error
	// Recovery は利用者が手で登録し直す手順。
	Recovery string
}

func (e *RegistrationError) Error() string {
	return fmt.Sprintf("herdr machine を登録できない: %v", e.Err)
}
func (e *RegistrationError) Unwrap() error { return e.Err }

// stopServer は VM 内の herdr server を止めるコマンド。
const stopServer = "pkill -x herdr"

// Register は sandbox VM を host の herdr に <sandbox の ssh の宛先> として登録する。
// kit が起動した VM 内の server を止めてから登録する (動いたままだと herdr machine add が
// "remote server is not ready for saved machines" で失敗する。ADR 0007 の実測)。
// 同じ宛先の登録が残っていたら (前の VM の解除の失敗など)、この回に作っていない登録には触れずに止める。
func (m Machines) Register(ctx context.Context, sandbox string, progress io.Writer) error {
	target := m.VM.SSHTarget(sandbox)
	recovery := fmt.Sprintf("sbx exec %s -- %s; %s", sandbox, stopServer, addCommand(target, sandbox))
	machines, err := m.Client.List(ctx)
	if err != nil {
		return &RegistrationError{Err: err, Recovery: recovery}
	}
	if stale, ok := findTarget(machines, target); ok {
		return &RegistrationError{
			Err:      fmt.Errorf("%s の登録 %s が既にある (前の sandbox VM の残りなら解除してから登録し直す)", target, stale.ID),
			Recovery: removeCommand(stale.ID) + "; " + recovery,
		}
	}
	// server が動いていなければ pkill は 1 で終わるので、それは成功として扱う
	stop := runtime.SandboxCommand{Args: []string{"sh", "-c", stopServer + " || [ $? -eq 1 ]"}}
	if _, err := m.VM.ExecInSandbox(ctx, sandbox, stop); err != nil {
		return &RegistrationError{Err: fmt.Errorf("VM 内の herdr server を止められない: %w", err), Recovery: recovery}
	}
	if err := m.Client.Add(ctx, target, sandbox); err != nil {
		return &RegistrationError{Err: err, Recovery: recovery}
	}
	_, _ = fmt.Fprintf(progress, "herdr: %s を登録した\n", target)
	return nil
}

// DisableForStop は herdr machine を無効にしてから stop で sandbox VM を止める
// (有効なままだと herdr が繋ぎ直して VM が起動し直す。ADR 0007)。無効にしたら、有効に戻すコマンドを progress に出す。
// host に herdr が無いか、無効にできなければ stop を呼ばずに error。stop が失敗したら、VM は動いたままなので
// herdr から見失わないよう有効に戻す。登録が無ければ、警告して stop だけを呼ぶ。
func (m Machines) DisableForStop(ctx context.Context, sandbox string, stop func(context.Context) error, progress io.Writer) error {
	if err := m.RequireOnHost(); err != nil {
		return err
	}
	target := m.VM.SSHTarget(sandbox)
	machine, found, err := m.find(ctx, target)
	if err != nil {
		return fmt.Errorf("herdr machine を無効にできないので止めない: %w", err)
	}
	if !found {
		_, _ = fmt.Fprintf(progress, "herdr: 警告 %s の登録が無い (無効にするものが無いので、そのまま止める)\n", target)
		return stop(ctx)
	}
	if err := m.Client.Disable(ctx, machine.ID); err != nil {
		return fmt.Errorf("herdr machine %s を無効にできないので止めない: %w", machine.Target, err)
	}
	if err := stop(ctx); err != nil {
		if enableErr := m.Client.Enable(ctx, machine.ID); enableErr != nil {
			return fmt.Errorf("%w (無効にした herdr machine も有効に戻せない: %s で戻す: %v)", err, enableCommand(machine.ID), enableErr)
		}
		return err
	}
	_, _ = fmt.Fprintf(progress, "herdr: %s を無効にした (起動し直したら %s で有効に戻す)\n", machine.Target, enableCommand(machine.ID))
	return nil
}

// Remove は sandbox VM の herdr machine を解除する。登録が無ければ何もしない。
func (m Machines) Remove(ctx context.Context, sandbox string) error {
	machine, found, err := m.find(ctx, m.VM.SSHTarget(sandbox))
	if err != nil || !found {
		return err
	}
	if err := m.Client.Remove(ctx, machine.ID); err != nil {
		return fmt.Errorf("herdr machine %s を解除できない (%s で解除する): %w", machine.Target, removeCommand(machine.ID), err)
	}
	return nil
}

func (m Machines) find(ctx context.Context, target string) (Machine, bool, error) {
	machines, err := m.Client.List(ctx)
	if err != nil {
		return Machine{}, false, err
	}
	machine, found := findTarget(machines, target)
	return machine, found, nil
}
