# 埋め込み資材は sandbox VM ごとの状態ディレクトリへ書き出す

Status: accepted (2026-09-25)

`sbx env rm` は渡した場所の env 定義を読み、lifecycle の相対 path のコマンドはその env 定義のディレクトリを cwd にして走る。そのため destroy の時点でも env 定義が実在している必要がある。single binary に埋め込んだ資材（env 定義と kit）は、create 時に `${XDG_STATE_HOME:-~/.local/state}/sbxr/sandboxes/<name>/` へ書き出し、destroy もそこを使って最後に消す。

ここには作成時に確定した宣言も残し、drift 検出の基準にする。

状態ディレクトリは sbxr が作った sandbox VM の印も兼ねる。

- 状態ディレクトリの無い sandbox VM は sbxr の管理外として、create・stop・destroy のどれでも触らない
- 状態ディレクトリには sandbox VM の出所（repo の path か git URL）を記録する。同じ名前の別 repo の VM を取り違えないためで、出所が違えば拒否する
- 作成時の宣言（`declaration.yaml`）は作成がすべて済んでから書き、作成が終わった印にする。印の無い状態ディレクトリは作成が途中で止まったものとして扱い、create はやり直さずに destroy を促す

destroy は、稼働中の sandbox VM を `--force` 無しでは撤去しない（実測は下記）。sbx の撤去は非対話では `--force` を要し、`--force` は使用中の VM も消す。一方で、使用中かを知る手段が無い。退けた案は次の 2 つ。
- 常に `--force` を渡す: `--force` を使用中の VM の強制撤去だけに使う、という CLI の約束が守れない
- `sbx stop` してから撤去する: `sbx stop` も使用中の session を切るので、`--force` と同じことになる

lifecycle から呼ぶ処理は `sbxr` の隠しサブコマンドにし、書き出す資材を減らす。

boot は埋め込みの kit `sbxr-boot`（状態ディレクトリの `kits/` に書き出し、env 定義から相対 path で指す）で、sandbox VM の起動ごとに再生する。

- create 時に確定した boot を、VM の `~/.config/sbxr/boot.sh` に書く。kit の startup は起動ごとにそれを実行するだけで、起動のたびに宣言を読み直さない
- create 時の kit startup は boot.sh が書かれる前に走るので、create 時の 1 回は sbxr が実行する
- VM 内へのファイルの書き込みは、`sbx exec -i` の stdin を VM 内の shell で書く。`sbx cp` は host の uid と mode のまま置くので、VM の agent から読めないことがある（旧実装の実測、sbx v0.43.0）

## Considered Options

- CLI の版ごとの共有ディレクトリ（`~/.local/share/sbxr/<version>/`）: create と destroy の間に sbxr を更新すると path が変わり、`env rm` が壊れる
- 一時ディレクトリ: destroy 時に存在しない

## 実測（2026-09-26、sbx v0.45.1、issue #5）

- lifecycle の相対 path のコマンドは、実行時に渡した env 定義のディレクトリを cwd にして走る（呼び出し元の cwd ではない）
- ディレクトリ A から `sbx env create` した後、A を B へ移しても `sbx env rm B` は通る。同じディレクトリである必要は無い
- A を削除すると `sbx env rm A` は `no sbxenv.yaml found` で失敗する。別のディレクトリに同じ `name:` の env 定義を書き直せば、そこからの `sbx env rm` は通る
- `sbx env rm` と `sbx rm` は、sandbox と一緒に sandbox スコープの secret（`sbx secret set --sandbox` で env 定義の外から置いたものを含む）と sandbox スコープ rule を消す
- sandbox スコープの secret は sandbox の作成前に置け、作成時に VM の環境変数へ placeholder が入る。env 定義の `env:` も VM の環境変数に入る。sandbox スコープ rule は sandbox の作成前には置けない（`sandbox not found`）
- sandbox を作る前に失敗し、sandbox スコープの secret だけが残った名前でも、`sbx env rm --force` は `not found` を出したうえで secret を消し、0 で終わる
- `sbx env rm` は stdin が端末でないと `--force` を要求し、`--force` は in-use の sandbox も消す。`sbx ls --json` に in-use を示す欄は無く、`sbx stop` も in-use を拒まない

前提の「同じ実在ディレクトリが要る」は「destroy の時点で env 定義が実在する」に弱まるが、状態ディレクトリに env 定義を置き続ける設計はそのまま成り立つ。

## 改訂（2026-09-26、#31）

設計ドキュメント（[docs/design/sbxr/](../design/sbxr/)）の決定で、上の記述を次のように改める。

- `--force` は、稼働中（使用中かもしれない）の VM の撤去に加えて、destroy 前の未回収の検査も省く。上の「`--force` を使用中の VM の強制撤去だけに使う」という約束は、この形に広がる（[decision/0003](../design/sbxr/decision/0003-destroy-checks-unrecovered.md)）
- 「lifecycle から呼ぶ処理は `sbxr` の隠しサブコマンドにし」は実装していない。env 定義は lifecycle を使わず、VM の中の処理は kit（bash）で完結する。sbx から sbxr への着信接続は無い
- 状態ディレクトリには、出所に加えて投入方式と herdr 連携の有無を作成の最初に記録する。作成途中の VM の stop と destroy もこれを読む
- 作成が終わった印（`declaration.yaml`）は、egress 自己検証が通った後に書く（[decision/0006](../design/sbxr/decision/0006-egress-self-check-from-vm.md)）
- 作成時の宣言があっても VM が無い（VM 消失）なら、create は drift を報告せず、destroy を促して止まる
- template を作る build 用 VM も状態ディレクトリを持ち、build の印と出所を記録する。状態ディレクトリの無い VM に触らないという約束は、build 用 VM の残骸の撤去にも及ぶ（[decision/0005](../design/sbxr/decision/0005-template-build-inputs-and-refresh.md)）
