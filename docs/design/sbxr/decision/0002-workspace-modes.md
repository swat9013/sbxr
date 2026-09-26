# 0002. repo の投入方式を clone・copy・mount から create の flag で選ぶ

- Status: Accepted
- Date: 2026-09-26
- 覆す条件: [system.md の未実測の前提](../system.md#未実測の前提) 4・5 が成り立たないとき

## 決定

- `sbxr create --workspace clone|copy|mount` で投入方式を選ぶ。既定は clone。plan も同じ flag を受ける
- repo 宣言と user 設定では選べない
- git URL の VM は clone だけを選べる
- copy は `.git`・tracked のファイル・ignored でない untracked のファイルを持ち込む。ignored のファイルは持ち込まない。`.git/config` は remote の URL（userinfo を除く）と branch の追跡設定だけを持ち込み、credential・`http.extraHeader`・include などの認証にかかわる設定は持ち込まない
- 選んだ方式は状態ディレクトリに記録し、drift を比べるときは記録した方式を再現する
- mount を選ぶと、確認関門で次の 2 つを示す: VM から host の作業ツリー（`.git` を含む）を書き換えられること、ignored のファイル（`.env`、個人設定の `.claude/settings.local.json` など）も VM から見えること

## 根拠

- untrusted な repo 宣言に、host の作業ツリーへの書き込み（mount）を選ばせない
- git URL で copy を選んでも、clone と同じ中身になる。mount は、destroy で消える cache clone を書き換えるだけになる
- ignored のファイルと `.git/config` の認証設定を除くのは、`.env` や URL に埋め込んだ token のような実値を VM に入れないため（ADR 0002 の趣旨）。VM の認証は secret の配線（placeholder 注入）に任せる
- mount は ADR 0002 の趣旨と、agent runtime profile に host の個人設定を持ち込まないこと（CONTEXT.md）を破りうる。利用者が flag で明示し、確認関門で示したときだけ許す

## 却下した代替案

- user 設定に既定を書き、flag で上書きする: 設定の置き場が増える割に、方式は VM ごとに選ぶもの
- repo 宣言でも選べる（mount を除く）: repo が利用者の作業ツリーの扱いを決めることになる
- git URL でも 3 方式を選べる（mount は cache clone を mount し、destroy はそれを残す）: 上の理由で意味が薄い
- 作業ツリーを丸ごと持ち込む: `.env` などの秘密も VM に入る
