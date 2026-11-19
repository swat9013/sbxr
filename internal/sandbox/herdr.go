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

// herdrRequired は、herdr 連携を有効にして作った sandbox VM かを返す。有効なら host に herdr があることも確かめ、無ければ error。
func (l Lifecycle) herdrRequired(name string) (bool, error) {
	enabled, err := l.herdrEnabled(name)
	if err != nil || !enabled {
		return false, err
	}
	return true, requireHerdrOnHost(l.Herdr)
}

func requireHerdrOnHost(client herdr.Client) error {
	if err := herdr.RequireOnHost(client); err != nil {
		return fmt.Errorf("herdr 連携が有効だが、%w。作成前なら user 設定で herdr.enabled: false にしてもよい", err)
	}
	return nil
}

// stopRunning は稼働中の sandbox VM を止める。herdr 連携を有効にして作った VM は、先に herdr machine を無効にする
// (有効なままだと herdr が繋ぎ直して VM が起動し直す。ADR 0007)。host に herdr が無ければ、VM に触れずに止まる。
// guide は、無効にした herdr machine を有効に戻す手順。
func (l Lifecycle) stopRunning(ctx context.Context, name string, guide enableGuide) error {
	enabled, err := l.herdrRequired(name)
	if err != nil {
		return err
	}
	if !enabled {
		return l.Runtime.StopSandbox(ctx, name)
	}
	enable, err := l.registry().DisableAndStop(ctx, name, l.Output)
	if err != nil {
		return err
	}
	l.reportDisabled(name, enable, guide)
	return nil
}

// disableHerdrMachine は止まっている sandbox VM の herdr machine を、VM に触れずに無効にする (decision/0010)。
// herdr 連携を有効にせずに作った VM では何もしない。
func (l Lifecycle) disableHerdrMachine(ctx context.Context, name string, guide enableGuide) error {
	enabled, err := l.herdrRequired(name)
	if err != nil || !enabled {
		return err
	}
	enable, err := l.registry().Disable(ctx, name, l.Output)
	if err != nil {
		return err
	}
	l.reportDisabled(name, enable, guide)
	return nil
}

// reportDisabled は無効にした herdr machine と、有効に戻す手順を示す。無効にしなかったら (enable が空) 何も出さない。
func (l Lifecycle) reportDisabled(name, enable string, guide enableGuide) {
	if enable != "" {
		logf(l.Output, "herdr: %s の herdr machine を無効にした (%s)\n", name, guide(enable))
	}
}

// enableGuide は、stop が無効にした herdr machine を有効に戻す手順を、手で戻す herdr のコマンド enable から組み立てる。
type enableGuide func(enable string) string

// viaStart は作成が終わった VM の手順: sbxr start が VM を起こして有効に戻す (decision/0025)。
// sbxr start が herdr machine の前で止まったとき (host に herdr が無い等) に備えて、手で戻すコマンドも添える。
func viaStart(repo string) enableGuide {
	return func(enable string) string {
		return fmt.Sprintf("sbxr start %s で VM を起こすと有効に戻る。手で戻すなら %s", repo, enable)
	}
}

// byHand は作成途中の VM の手順: sbxr start は作成途中の VM を拒むので、手で戻すコマンドだけを示す。
func byHand(enable string) string { return enable + " で有効に戻す" }

// startStopped は停止中の sandbox VM を起こし、herdr 連携を有効にして作った VM なら herdr machine を有効に戻す。
// host に herdr が無ければ、VM に触れずに止まる。herdr machine は VM を起こせた後に有効に戻す
// (起こせなかった VM の machine を有効にすると、herdr が繋ぎ直して VM を起こしうる)。
func (l Lifecycle) startStopped(ctx context.Context, name string) error {
	enabled, err := l.herdrRequired(name)
	if err != nil {
		return err
	}
	if err := l.Runtime.StartSandbox(ctx, name); err != nil {
		return fmt.Errorf("sandbox VM %s を起こせない (VM は止まったまま): %w", name, err)
	}
	if !enabled {
		return nil
	}
	if err := l.registry().Enable(ctx, name, l.Output); err != nil {
		return fmt.Errorf("%w\nsandbox VM %s は起こした", err, name)
	}
	return nil
}

// enableHerdrMachine は稼働中の sandbox VM の herdr machine を、VM に触れずに有効に戻す (decision/0025)。
// herdr 連携を有効にせずに作った VM では何もしない。host に herdr が無ければ error。
func (l Lifecycle) enableHerdrMachine(ctx context.Context, name string) error {
	enabled, err := l.herdrRequired(name)
	if err == nil && enabled {
		err = l.registry().Enable(ctx, name, l.Output)
	}
	if err != nil {
		return fmt.Errorf("%w\nsandbox VM %s は動いている (VM には触れていない)", err, name)
	}
	return nil
}

// removeHerdrMachine は herdr 連携を有効にして作った VM の herdr machine を解除する。登録が無ければ何もしない。
func (l Lifecycle) removeHerdrMachine(ctx context.Context, name string) error {
	enabled, err := l.herdrEnabled(name)
	if err != nil || !enabled {
		return err
	}
	return l.registry().Remove(ctx, name)
}
