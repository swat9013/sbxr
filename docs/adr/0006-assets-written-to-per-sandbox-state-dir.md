# 埋め込み資材は sandbox VM ごとの状態ディレクトリへ書き出す

Status: accepted (2026-09-25)

`sbx env create` と `sbx env rm` には同じ実在のディレクトリを渡す必要がある（lifecycle が相対 path でコマンドを呼ぶため）。single binary に埋め込んだ資材（env 定義と kit）は、create 時に `${XDG_STATE_HOME:-~/.local/state}/sbxr/sandboxes/<name>/` へ書き出し、destroy もそこを使って最後に消す。

`sbx env rm` は渡した場所の env 定義を読むので、destroy の時点で env 定義が実在している必要がある（実測は下記）。ここには作成時に確定した宣言も残し、drift 検出の基準にする。lifecycle から呼ぶ処理は `sbxr` の隠しサブコマンドにし、書き出す資材を減らす。

## Considered Options

- CLI の版ごとの共有ディレクトリ（`~/.local/share/sbxr/<version>/`）: create と destroy の間に sbxr を更新すると path が変わり、`env rm` が壊れる
- 一時ディレクトリ: destroy 時に存在しない

## 実測（2026-09-26、sbx v0.45.1、issue #5）

- lifecycle の相対 path のコマンドは、実行時に渡した env 定義のディレクトリを cwd にして走る（呼び出し元の cwd ではない）
- ディレクトリ A から `sbx env create` した後、A を B へ移しても `sbx env rm B` は通る。同じディレクトリである必要は無い
- A を削除すると `sbx env rm A` は `no sbxenv.yaml found` で失敗する。別のディレクトリに同じ `name:` の env 定義を書き直せば、そこからの `sbx env rm` は通る
- `sbx env rm` と `sbx rm` は、sandbox と一緒に sandbox スコープの secret（`sbx secret set --sandbox` で env 定義の外から置いたものを含む）と sandbox スコープ rule を消す
- sandbox スコープの secret は sandbox の作成前に置け、作成時に VM の環境変数へ placeholder が入る。env 定義の `env:` も VM の環境変数に入る。sandbox スコープ rule は sandbox の作成前には置けない（`sandbox not found`）
- `sbx env rm` は stdin が端末でないと `--force` を要求し、`--force` は in-use の sandbox も消す。`sbx ls --json` に in-use を示す欄は無く、`sbx stop` も in-use を拒まない

前提の「同じ実在ディレクトリが要る」は「destroy の時点で env 定義が実在する」に弱まるが、状態ディレクトリに env 定義を置き続ける設計はそのまま成り立つ。
