# secret は定義と要求を分け、repo 宣言は名前で要求するだけにする

Status: accepted (2026-09-25)

repo 宣言（`sbxr.yaml`）は untrusted な入力である。repo 宣言が注入先 host を書けると、悪意のある repo が「利用者の token をこの host へ送れ」と指定できる。そこで secret を 2 つの key に分ける。

- **secret 定義** `secret_defs.<name>`（default / user スコープのみ）: 注入方式（sbx 組み込み service か、host 指定の placeholder 注入か）、secret ファイルのキー、注入先 host、VM の環境変数名、秘密でない付随値（`vars`: host・site・email 等）を持つ
- **secret 要求** `secrets: [<name>]`（全スコープ）: 名前の list。スコープ間は和集合。user スコープの要求は全 sandbox VM への常時要求になる（例: `github`）

配線するのは、要求され、かつ注入先 host が egress で許可されているときに限る。要求された名前に定義が無ければ create を error で止め、足りない名前を表示する。GitHub の定義は default スコープに同梱し、利用者は secret ファイルに値を書いて user スコープで要求するだけで使える。

旧実装は GitHub を全 VM に無条件で、GitLab を repo の origin host から、Atlassian を repo の egress 許可から、それぞれ暗黙に配線していた。これを名前による明示要求に置き換える。repo 宣言に書く名前は各利用者の定義を指すため、同じ repo を使う利用者の間では名前が契約になる。

## Considered Options

- 1 つの key `secrets` に定義と要求を同居させ、定義に `always: true` を持たせる: key は 1 つ減るが flag が増え、repo 層の制限が key 単位からフィールド単位へ細かくなる
