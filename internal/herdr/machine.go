package herdr

import (
	"context"
	"fmt"
	"io"

	"github.com/swat9013/sbxr/internal/runtime"
)

// VM は herdr machine の登録簿が sandbox VM に求めるもの: host から繋ぐ ssh の宛先、VM 内のコマンドの実行、VM の停止。
type VM interface {
	SSHTarget(sandbox string) string
	ExecInSandbox(ctx context.Context, sandbox string, command runtime.SandboxCommand) ([]byte, error)
	StopSandbox(ctx context.Context, sandbox string) error
}

// Registry は sandbox VM ごとの herdr machine の登録簿: 登録・停止前の無効化・解除 (ADR 0007)。
// 残った登録の扱いと、失敗したときの復旧手順の文面はここに置く。host に herdr があることは、呼び出し側が
// 先に RequireOnHost で確かめる (確認関門の前に止めるため)。
type Registry struct {
	Client Client
	VM     VM
}

// RequireOnHost は host に herdr があることを確かめる。無ければ、herdr を入れる手順を添えた error。
func RequireOnHost(client Client) error {
	if err := client.Available(); err != nil {
		return fmt.Errorf("%w (PATH に herdr を入れる)", err)
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

// Register は sandbox VM を host の herdr に、その ssh の宛先で登録する。
// kit が起動した VM 内の server を止めてから登録する (動いたままだと herdr machine add が
// "remote server is not ready for saved machines" で失敗する。ADR 0007 の実測)。
// 同じ宛先の登録が残っていたら (前の VM の解除の失敗など)、この回に作っていない登録には触れずに止める。
func (r Registry) Register(ctx context.Context, sandbox string, progress io.Writer) error {
	target := r.VM.SSHTarget(sandbox)
	recovery := fmt.Sprintf("VM %s の中で %s を実行してから %s", sandbox, stopServer, addCommand(target, sandbox))
	machines, err := r.Client.List(ctx)
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
	if _, err := r.VM.ExecInSandbox(ctx, sandbox, stop); err != nil {
		return &RegistrationError{Err: fmt.Errorf("VM 内の herdr server を止められない: %w", err), Recovery: recovery}
	}
	if err := r.Client.Add(ctx, target, sandbox); err != nil {
		return &RegistrationError{Err: err, Recovery: recovery}
	}
	logf(progress, "herdr: %s を登録した\n", target)
	return nil
}

// DisableAndStop は herdr machine を無効にしてから sandbox VM を止める
// (有効なままだと herdr が繋ぎ直して VM が起動し直す。ADR 0007)。返り値は有効に戻すコマンド (無効にしなかったら空)。
// 無効にできなければ止めずに error。止められなければ、VM は動いたままなので herdr から見失わないよう、
// sbxr が無効にした machine を有効に戻す。登録が無ければ、警告して止めるだけにする。
// 無効にしたことと戻し方の案内は、返り値を受けた呼び出し側が出す。
func (r Registry) DisableAndStop(ctx context.Context, sandbox string, progress io.Writer) (enable string, err error) {
	target := r.VM.SSHTarget(sandbox)
	machine, found, err := r.find(ctx, target)
	if err != nil {
		return "", fmt.Errorf("herdr machine を無効にできないので止めない: %w", err)
	}
	if !found {
		logf(progress, "herdr: 警告 %s の登録が無い (無効にするものが無いので、そのまま止める)\n", target)
		return "", r.VM.StopSandbox(ctx, sandbox)
	}
	if !machine.Enabled { // 利用者が無効にしたものは、そのままにする
		return "", r.VM.StopSandbox(ctx, sandbox)
	}
	if err := r.Client.Disable(ctx, machine.ID); err != nil {
		return "", fmt.Errorf("herdr machine %s を無効にできないので止めない: %w", machine.Target, err)
	}
	enable = enableCommand(machine.ID)
	if err := r.VM.StopSandbox(ctx, sandbox); err != nil {
		// 止める側の中断 (Ctrl-C など) で、戻す操作まで止めない
		if enableErr := r.Client.Enable(context.WithoutCancel(ctx), machine.ID); enableErr != nil {
			return "", fmt.Errorf("%w (無効にした herdr machine も有効に戻せない: %s で戻す: %v)", err, enable, enableErr)
		}
		return "", err
	}
	return enable, nil
}

// Remove は sandbox VM の herdr machine を解除する。登録が無ければ何もしない。
func (r Registry) Remove(ctx context.Context, sandbox string) error {
	machine, found, err := r.find(ctx, r.VM.SSHTarget(sandbox))
	if err != nil || !found {
		return err
	}
	if err := r.Client.Remove(ctx, machine.ID); err != nil {
		return fmt.Errorf("herdr machine %s を解除できない (%s で解除する): %w", machine.Target, removeCommand(machine.ID), err)
	}
	return nil
}

func (r Registry) find(ctx context.Context, target string) (Machine, bool, error) {
	machines, err := r.Client.List(ctx)
	if err != nil {
		return Machine{}, false, err
	}
	machine, found := findTarget(machines, target)
	return machine, found, nil
}

func logf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
