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
// repo は、有効に戻す手順 (sbxr start) に添える利用者の <repo>。
func (l Lifecycle) stopRunning(ctx context.Context, name, repo string) error {
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
	l.reportDisabled(name, repo, enable)
	return nil
}

// disableHerdrMachine は止まっている sandbox VM の herdr machine を、VM に触れずに無効にする (decision/0010)。
// herdr 連携を有効にせずに作った VM では何もしない。
func (l Lifecycle) disableHerdrMachine(ctx context.Context, name, repo string) error {
	enabled, err := l.herdrRequired(name)
	if err != nil || !enabled {
		return err
	}
	enable, err := l.registry().Disable(ctx, name, l.Output)
	if err != nil {
		return err
	}
	l.reportDisabled(name, repo, enable)
	return nil
}

// reportDisabled は無効にした herdr machine と、VM と一緒に有効に戻すコマンド (sbxr start。decision/0025) を示す。
// 無効にしなかったら (enable が空) 何も出さない。手で有効に戻すコマンドは示さない (VM が止まったまま有効にすると、
// herdr が繋ぎ直して sbxr の外で VM を起こす)。
func (l Lifecycle) reportDisabled(name, repo, enable string) {
	if enable != "" {
		logf(l.Output, "herdr: %s の herdr machine を無効にした (sbxr start %s で VM を起こすと有効に戻る)\n", name, repo)
	}
}

// startStopped は停止中の sandbox VM を起こし、VM の中を確かめてから、herdr 連携を有効にして作った VM なら
// herdr machine を有効に戻し、最後に run (作成時の宣言のもの) を worktree で始める。host に herdr が無ければ、VM に触れずに止まる。
// herdr machine は VM を起こせた後に有効に戻す (起こせなかった VM の machine を有効にすると、herdr が繋ぎ直して VM を起こしうる)。
// repo は、確かめ直す手順 (sbxr start) に添える利用者の <repo>。
func (l Lifecycle) startStopped(ctx context.Context, name, repo, worktree string, run []string) error {
	enabled, err := l.herdrRequired(name)
	if err != nil {
		return err
	}
	if err := l.Runtime.StartSandbox(ctx, name); err != nil {
		return fmt.Errorf("sandbox VM %s を起こせない (VM は止まったまま): %w", name, err)
	}
	if err := l.readyStarted(ctx, name, repo, enabled); err != nil {
		// VM は稼働中になったので、確かめ直す start は run を始めない。run を始めるには起こし直す
		return fmt.Errorf("%w\nsandbox VM %s は起こした%s", err, name, runNotStarted(repo, run))
	}
	if err := startRun(ctx, l.Runtime, name, worktree, run, l.Output); err != nil {
		return fmt.Errorf("%w\nsandbox VM %s は起こした。%s", err, name, restartRun(repo))
	}
	return nil
}

// startRunning は稼働中の sandbox VM を起こさずに、VM の中を確かめてから herdr machine を有効に戻す (decision/0025)。
// host に herdr が無ければ、VM に触れずに止まる。
func (l Lifecycle) startRunning(ctx context.Context, name, repo string) error {
	enabled, err := l.herdrRequired(name)
	if err != nil {
		return err
	}
	if err := l.readyStarted(ctx, name, repo, enabled); err != nil {
		return fmt.Errorf("%w\nsandbox VM %s は動いている (VM には触れていない)", err, name)
	}
	return nil
}

// readyStarted は稼働中の sandbox VM の中で agent と Docker が使えることを確かめ (decision/0027)、
// herdr 連携を有効にして作った VM なら herdr machine を有効に戻す。確かめた項目が成り立たなければ、
// herdr machine を有効に戻さずに、確かめ直す手順を添えて止まる (UC10 の 4a。VM は稼働中のまま)。
func (l Lifecycle) readyStarted(ctx context.Context, name, repo string, herdrEnabled bool) error {
	if err := checkDevTools(ctx, l.Runtime, name, l.Output); err != nil {
		var untouched string
		if herdrEnabled { // 有効か無効かは読んでいないので、変えていないことだけを示す
			untouched = " (herdr machine の有効・無効は変えていない)"
		}
		return fmt.Errorf("%w\n直してから sbxr start %s で確かめ直す%s", err, repo, untouched)
	}
	if !herdrEnabled {
		return nil
	}
	return l.registry().Enable(ctx, name, l.Output)
}

// removeHerdrMachine は herdr 連携を有効にして作った VM の herdr machine を解除する。登録が無ければ何もしない。
func (l Lifecycle) removeHerdrMachine(ctx context.Context, name string) error {
	enabled, err := l.herdrEnabled(name)
	if err != nil || !enabled {
		return err
	}
	return l.registry().Remove(ctx, name)
}
