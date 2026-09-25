# herdr 連携は opt-in の専用 key として CLI に組み込む

Status: accepted (2026-09-26)

herdr 連携は、sandbox VM に herdr を入れ、host の herdr から VM 内の agent を扱えるようにする支援機能である。これを user 設定の専用 key `herdr`（`enabled` / `version`）で opt-in にし、sbxr 本体に組み込む。default スコープでは `enabled: false` にして、動作確認済みの版を `version` に固定する。repo 宣言は `herdr` を書けない。untrusted な repo に、host の herdr へ machine を登録させないため。

有効にすると、VM 内では起動ごとに次を行う。herdr の導入（認証の要らない GitHub release の取得で、`secrets` の要求に依存しない）、server の起動、Claude Code への integration の導入。このため agent runtime profile は、宣言の `profile` と herdr 連携の両方から作られる。host 側では、sbxr 本体が herdr machine を create 後に登録し、stop 前に無効化し、destroy 前に解除する。stop 前に無効化するのは、有効なままだと herdr が繋ぎ直して VM が再起動するため。

登録と解除は sbx の lifecycle からは呼ばない。旧実装では lifecycle から呼ぶと HOME と TTY が無く、kit の起動も待たないので失敗していた。有効なのに host に herdr が無ければ、確認関門の前に error で止める。

## 実装で決めたこと（#7）

- VM 内の処理は、埋め込みの kit `sbxr-herdr` を repo の boot の kit より前に置いて行う。sbx の VM の dispatcher（`/etc/durable-startup.d/run.sh`）は、段が 0 以外で終わるとそこで止まる。そのため herdr の kit の各段は、失敗しても 0 で終わり、`sbxr-herdr: fail <段>` の行を `/var/log/sbx-kit-startup.log` に残す。herdr の失敗で repo の boot を飛ばさないためである。create はこの行と dispatcher の `fail` 行を見て、VM の中の段の失敗として止まる。2 回目以降の起動での失敗は、boot と同じく log に残るだけになる。
- 登録の前に、kit が起動した VM 内の herdr server を止める。動いたままだと `herdr machine add` が `remote server is not ready for saved machines` で失敗し、止めてから add すると登録できる（herdr 0.9.1 / VM の herdr v0.9.0 で実測）。
- 有効にして作ったかは、状態ディレクトリの env 定義に herdr の kit があるかで判定する。env 定義は作成の最初に書くので、途中で止まった作成を destroy するときにも読める。
- create で同じ `<name>.sbx` の登録が既にあれば（前の VM の解除の失敗の残りなど）、その回に作っていない登録には触れずに止める。表示する手順で、利用者が解除してから登録し直す。
- 有効にして作った VM の stop と destroy も、host に herdr が無ければ確認や VM の操作の前に error で止める。無効化や解除をせずに進めると、登録が残るためである。
- stop は、machine を無効にできなければ VM を止めない。止めても herdr が繋ぎ直して起動し直すためである。無効にした後で VM を止められなかったときは、machine を有効に戻す。VM は動いたままなので、herdr から見失わせないためである。

## Considered Options

- 汎用の host hook（user スコープだけが書ける、create 後・stop 前・destroy 前に host で走るコマンド）と、user スコープの `init` / `boot` で組み立てる: sbxr に herdr の語が入らない。しかし lifecycle の癖と stop 時の無効化を利用者の設定で吸収させることになる。
- v0.1 から外す: 範囲は最小になる。しかし作者の日常の利用経路が v0.1 で欠ける。
