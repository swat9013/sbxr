package sandbox

import (
	"context"
	"fmt"

	"github.com/swat9013/sbxr/internal/herdr"
)

// herdrEnabled は sandbox VM を herdr 連携を有効にして作ったかを、状態ディレクトリの記録から返す。
func (h Hosts) herdrEnabled(dir stateDir) (bool, error) {
	return dir.herdrEnabled(h.Runtime.DefinedWithHerdr)
}

// RequireHerdr は herdr 連携が有効なら host に herdr があることを確かめる。無効なら herdr を呼ばない。
func (p Prepared) RequireHerdr(client herdr.Client) error {
	if p.Declaration.Herdr == nil {
		return nil
	}
	return requireHerdrOnHost(client)
}

// RequireHerdrFor は、herdr 連携を有効にして作った sandbox VM なら host に herdr があることを確かめる。
// herdr 連携の有無は、作成の最初の記録から読む (作成が途中で止まった VM でも読める)。
// destroy が確認の前に呼ぶ (stop は Stop の中で確かめる)。
func RequireHerdrFor(hosts Hosts, places Places, name string) error {
	enabled, err := hosts.herdrEnabled(places.stateDirOf(name))
	if err != nil || !enabled {
		return err
	}
	return requireHerdrOnHost(hosts.Herdr)
}

func requireHerdrOnHost(client herdr.Client) error {
	if err := herdr.RequireOnHost(client); err != nil {
		return fmt.Errorf("herdr 連携が有効だが、%w。作成前なら user 設定で herdr.enabled: false にしてもよい", err)
	}
	return nil
}

// removeHerdrMachine は herdr 連携を有効にして作った VM の herdr machine を解除する。登録が無ければ何もしない。
func removeHerdrMachine(ctx context.Context, hosts Hosts, dir stateDir, name string) error {
	enabled, err := hosts.herdrEnabled(dir)
	if err != nil || !enabled {
		return err
	}
	return hosts.registry().Remove(ctx, name)
}
