# 0015. herdr の最初の workspace は、登録の後に VM 内の作業ツリーで開き直す

- Status: Accepted
- Date: 2026-09-29
- 覆す条件: `herdr machine add` が、VM 内の server の cwd か最初の workspace の cwd を指定する手段を持ったとき。または、kit が起動した server の pane が ssh 経由で起動した server の pane と同じ環境（proxy・PATH・SHELL）を持てるようになったとき

## 決定

[decision/0014](0014-vm-worktree-at-host-path-and-herdr-starts-there.md) の「herdr の pane を VM 内の作業ツリーから始める」を、create の経路で次のように実現する。

- create は herdr machine を登録した後、VM 内の herdr CLI で次の 2 つを行う
  - VM 内の作業ツリーを `--cwd` にした workspace を開く
  - 開く前からあった workspace（server が起動時に作ったもの）を閉じる
- 開く順と閉じる順はこのとおりにする。workspace が 0 件になる間を作らない
- 開いた workspace の最初の pane が VM 内の作業ツリーで始まったことを、`workspace create` の応答で確かめる。違えば create を失敗にする
- この段で失敗しても VM と herdr machine の登録は残す。手で開き直すコマンドを示して、非 0 で終える。示すコマンドは、開けたときだけ閉じる形（`&&`）にする。この失敗は herdr machine の登録の失敗とは別の error として扱う（済んだことが違い、復旧は VM 内の workspace の操作だけになる）
- 登録の前に kit の server を止める手順（[ADR 0007](../../../adr/0007-herdr-as-opt-in-dedicated-key.md)）は変えない
- decision/0014 が kit に求めた 2 つ（`new_cwd` の設定と、server を VM 内の作業ツリーから起動すること）は、そのまま kit が行う
  - `new_cwd`: `--cwd` なしで作る pane・tab・workspace に効く
  - server の起動: 起動し直した VM で、保存された session が無いときの最初の workspace に効く
- kit が書く `~/.config/herdr/config.toml` は sbxr が持つ。起動ごとに書き直す
- kit は、VM 内の作業ツリーが無いか入れないとき、`config.toml` を書けないときに `sbxr-herdr: fail` 行を残す。create はこの行を VM の中の段の失敗として止まる（ADR 0007 の他の段と同じ扱い）。create を止めるのは、server を起動する前の段の行。server を起動する段は background なので、作業ツリーに入れなかったときの行は起動し直した VM の log に残るだけで、server は既定の cwd で起動する
- kit は VM 内の作業ツリーの path を bash の単一引用符の中へ差し込み、TOML の文字列として書く。このため、単一引用符・制御文字・UTF-8 でない byte を含む path の repo は、herdr 連携を有効にした create で env 定義を書く前に拒む（出所を記録する前なので、[decision/0011](0011-create-failure-before-source-returns-to-absent.md) のとおり未作成に戻る）
- 最初の pane が作業ツリーで始まったかは、`workspace create` の応答の `cwd` と作業ツリーの path を文字列で比べて確かめる。実測では、応答の `cwd` は渡した path と同じだった。symlink を含む path で herdr が正規化した値を返すかは確かめていない

## 根拠

実測（2026-09-29、sbx v0.46.0、host の herdr 0.9.1、VM の herdr v0.9.0、clone 方式の probe の VM）の結果による。

- system.md の未実測の前提 9 は成り立たない
  - VM に `new_cwd = "<作業ツリー>"` を書いて create と同じ手順（server を止め、session を消し、`herdr machine add`）を通した
  - 起動した server の cwd は `/home/agent/workspace` のままだった
  - server の log は `created startup workspace cwd=/home/agent/workspace` で、最初の workspace は server の cwd で始まった
  - `new_cwd` は最初の workspace に効かない
- `new_cwd` は、`--cwd` なしで作る tab・workspace・pane の分割には効いた
  - VM 内の `herdr tab create`・`herdr workspace create`・`herdr pane split` の応答の `cwd` が、どれも作業ツリーだった
- 登録の後に開き直す方法で、最初の pane が作業ツリーで始まった
  - `herdr workspace create --cwd <作業ツリー> --focus` の後に `herdr workspace close w1` を実行した
  - その後の `herdr workspace list` は作業ツリーの workspace 1 件だけだった
  - その pane の中で実行した結果: shell は `/bin/bash`、`pwd` は作業ツリー、`command -v claude` は `/home/agent/.local/bin/claude`、`HTTPS_PROXY` は sbx の proxy
- 登録で起動する server だけが、pane に要る環境を持つ
  - `herdr machine add` が ssh 経由で起動した server の environ: `SHELL=/bin/bash`、PATH の先頭に `/home/agent/.local/bin` と `/usr/local/share/npm-global/bin`、`HTTP(S)_PROXY`・`SSH_AUTH_SOCK`・`BASH_ENV`・`NODE_EXTRA_CA_CERTS`・`HERDR_STARTUP_CWD=/home/agent/workspace`
  - kit が起動した server の environ: `SHELL=/bin/sh`、PATH に `~/.local/bin` が無い
  - kit と同じ環境から login shell を通しても、PATH は変わらなかった
  - pane はこの environ を継ぐので、kit の server の pane では `claude` と proxy が見つからない
- 開き直すのは sbxr が VM 内の herdr CLI を呼ぶだけで済む。VM 内の herdr は socket で自分の server に繋ぐ。host の herdr から VM の server を操作する経路（`herdr --machine`）は、VM の herdr v0.9.0 が対応していない（`remote Herdr does not support machine API forwarding`）

## 却下した代替案

- 登録の前に kit の server を止めず、作業ツリーで起動した kit の server に `machine add` を繋ぐ
  - 最初の workspace は作業ツリーで始まる（実測）
  - ただし、pane が kit の環境を継ぐので、上の根拠のとおり `claude` と proxy を失う
  - 実測で分かったこと: kit の server に `machine add` すると `error: remote server is not ready for saved machines; machine was not saved` で失敗する。`~/.local/bin/herdr server` で起動した server なら、動いたままでも `Remote server is ready.` で登録できた。kit は `/usr/local/bin/herdr` を起動する。どちらの binary も v0.9.0 で、差は起動した path と環境にあった。herdr の中で何を検査しているかは確かめていない
- 登録の後に、最初の pane へ `cd <作業ツリー>` を送る
  - pane の履歴にコマンドが残る
  - workspace の名前（herdr が最初の cwd から付ける）が `workspace` のまま残る
- `new_cwd` だけを書く: 最初の workspace に効かない（上の実測）
- sbxr が server を止めた後、作業ツリーから起動し直してから登録する
  - `sbx exec` は detach に対応していない（`-d` は `not supported`）
  - `sbx exec` の環境にも `SHELL` が無く、pane は `/bin/sh` になった（実測）
