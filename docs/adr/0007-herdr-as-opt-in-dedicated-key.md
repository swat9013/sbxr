# herdr 連携は opt-in の専用 key として CLI に組み込む

Status: accepted (2026-09-26)

herdr 連携は、sandbox VM に herdr を入れ、host の herdr から VM 内の agent を扱えるようにする支援機能である。これを user 設定の専用 key `herdr`（`enabled` / `version`）で opt-in にし、sbxr 本体に組み込む。default スコープでは `enabled: false` にして、動作確認済みの版を `version` に固定する。repo 宣言は `herdr` を書けない。untrusted な repo に、host の herdr へ machine を登録させないため。

有効にすると、VM 内では起動ごとに次を行う。herdr の導入（認証の要らない GitHub release の取得で、`secrets` の要求に依存しない）、server の起動、Claude Code への integration の導入。このため agent runtime profile は、宣言の `profile` と herdr 連携の両方から作られる。host 側では、sbxr 本体が herdr machine を create 後に登録し、stop 前に無効化し、destroy 前に解除する。stop 前に無効化するのは、有効なままだと herdr が繋ぎ直して VM が再起動するため。

登録と解除は sbx の lifecycle からは呼ばない。旧実装では lifecycle から呼ぶと HOME と TTY が無く、kit の起動も待たないので失敗していた。有効なのに host に herdr が無ければ、確認関門の前に error で止める。

## Considered Options

- 汎用の host hook（user スコープだけが書ける、create 後・stop 前・destroy 前に host で走るコマンド）と、user スコープの `init` / `boot` で組み立てる: sbxr に herdr の語が入らない。しかし lifecycle の癖と stop 時の無効化を利用者の設定で吸収させることになる。
- v0.1 から外す: 範囲は最小になる。しかし作者の日常の利用経路が v0.1 で欠ける。
