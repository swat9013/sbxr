package sandbox

import (
	"context"
	"io"

	"github.com/swat9013/sbxr/internal/herdr"
	"github.com/swat9013/sbxr/internal/runtime"
)

// Hosts は sbxr が扱う外部: sandbox VM の実行基盤と host の herdr。
type Hosts struct {
	Runtime runtime.Runtime
	Herdr   herdr.Client
}

// machines は sandbox VM の herdr machine を扱う。
func (h Hosts) machines() herdr.Machines {
	return herdr.Machines{Client: h.Herdr, VM: h.Runtime}
}

// RequireHerdr は herdr 連携が有効なら host に herdr があることを確かめる。無効なら herdr を呼ばない。
func (p Prepared) RequireHerdr(hosts Hosts) error {
	if p.Declaration.Herdr == nil {
		return nil
	}
	return hosts.machines().RequireOnHost()
}

// RequireHerdrFor は、herdr 連携を有効にして作った sandbox VM なら host に herdr があることを確かめる。
// herdr 連携の有無は、作成の最初の記録から読む (作成が途中で止まった VM でも読める)。
// destroy が確認の前に呼ぶ。
func RequireHerdrFor(hosts Hosts, places Places, name string) error {
	enabled, err := places.stateDirOf(name).herdrEnabled(hosts.Runtime.DefinedWithHerdr)
	if err != nil || !enabled {
		return err
	}
	return hosts.machines().RequireOnHost()
}

// removeHerdrMachine は herdr 連携を有効にして作った VM の herdr machine を解除する。登録が無ければ何もしない。
func removeHerdrMachine(ctx context.Context, hosts Hosts, dir stateDir, name string) error {
	enabled, err := dir.herdrEnabled(hosts.Runtime.DefinedWithHerdr)
	if err != nil || !enabled {
		return err
	}
	return hosts.machines().Remove(ctx, name)
}

// Stop は sandbox VM を止める。herdr 連携を有効にして作った VM は、先に herdr machine を無効にする (ADR 0007)。
func Stop(ctx context.Context, hosts Hosts, places Places, name string, progress io.Writer) error {
	enabled, err := places.stateDirOf(name).herdrEnabled(hosts.Runtime.DefinedWithHerdr)
	if err != nil {
		return err
	}
	stop := func(ctx context.Context) error { return hosts.Runtime.StopSandbox(ctx, name) }
	if !enabled {
		return stop(ctx)
	}
	return hosts.machines().DisableForStop(ctx, name, stop, progress)
}
