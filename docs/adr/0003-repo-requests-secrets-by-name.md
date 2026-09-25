# repo 宣言は secret を名前で要求するだけにする

Status: accepted (2026-09-25)

repo 宣言（`sbxr.yaml`）は untrusted な入力である。repo 宣言が注入先 host を書けると、悪意のある repo が「利用者の token をこの host へ送れ」と指定できる。そこで secret 定義（取得元キー・注入先 host・VM の環境変数名）は user 設定だけが持ち、repo 宣言は `secrets: [<name>]` で名前を要求するだけにする。配線するのは、名前で要求され、かつその注入先 host が egress で許可されているときに限る。

旧実装は「repo が許可した host」と「keychain に secret がある host」の重なりで暗黙に配線していた。これを名前による明示要求に置き換える。
