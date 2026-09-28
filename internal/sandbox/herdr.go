package sandbox

import (
	"context"
	"fmt"

	"github.com/swat9013/sbxr/internal/herdr"
)

func (l Lifecycle) registry() herdr.Registry {
	return herdr.Registry{Client: l.Herdr, VM: l.Runtime}
}

// herdrEnabled は sandbox VM を herdr 連携を有効にして作ったかを、作成の最初の記録から返す (作成途中の VM でも読める)。
func (l Lifecycle) herdrEnabled(name string) (bool, error) {
	return l.Places.stateDirOf(name).herdrEnabled(l.Runtime.DefinedWithHerdr)
}

// requireHerdrFor は、herdr 連携を有効にして作った sandbox VM なら host に herdr があることを確かめる。
func (l Lifecycle) requireHerdrFor(name string) error {
	enabled, err := l.herdrEnabled(name)
	if err != nil || !enabled {
		return err
	}
	return requireHerdrOnHost(l.Herdr)
}

func requireHerdrOnHost(client herdr.Client) error {
	if err := herdr.RequireOnHost(client); err != nil {
		return fmt.Errorf("herdr 連携が有効だが、%w。作成前なら user 設定で herdr.enabled: false にしてもよい", err)
	}
	return nil
}

// stopRunning は稼働中の sandbox VM を止める。herdr 連携を有効にして作った VM は、先に herdr machine を無効にする
// (有効なままだと herdr が繋ぎ直して VM が起動し直す。ADR 0007)。host に herdr が無ければ、VM に触れずに止まる。
func (l Lifecycle) stopRunning(ctx context.Context, name string) error {
	enabled, err := l.herdrEnabled(name)
	if err != nil {
		return err
	}
	if !enabled {
		return l.Runtime.StopSandbox(ctx, name)
	}
	if err := requireHerdrOnHost(l.Herdr); err != nil {
		return err
	}
	enable, err := l.registry().DisableAndStop(ctx, name, l.Output)
	if err != nil {
		return err
	}
	l.reportDisabled(name, enable)
	return nil
}

// disableHerdrMachine は止まっている sandbox VM の herdr machine を、VM に触れずに無効にする (decision/0010)。
// herdr 連携を有効にせずに作った VM では何もしない。
func (l Lifecycle) disableHerdrMachine(ctx context.Context, name string) error {
	enabled, err := l.herdrEnabled(name)
	if err != nil || !enabled {
		return err
	}
	if err := requireHerdrOnHost(l.Herdr); err != nil {
		return err
	}
	enable, err := l.registry().Disable(ctx, name, l.Output)
	if err != nil {
		return err
	}
	l.reportDisabled(name, enable)
	return nil
}

// reportDisabled は無効にした herdr machine と、有効に戻すコマンドを示す。無効にしなかったら (enable が空) 何も出さない。
func (l Lifecycle) reportDisabled(name, enable string) {
	if enable != "" {
		logf(l.Output, "herdr: %s の herdr machine を無効にした (起動し直したら %s で有効に戻す)\n", name, enable)
	}
}

// removeHerdrMachine は herdr 連携を有効にして作った VM の herdr machine を解除する。登録が無ければ何もしない。
func (l Lifecycle) removeHerdrMachine(ctx context.Context, name string) error {
	enabled, err := l.herdrEnabled(name)
	if err != nil || !enabled {
		return err
	}
	return l.registry().Remove(ctx, name)
}
