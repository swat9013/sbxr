# 0004. template は repo ごとに create の中で作り、新しいものを作ったら旧いものを消す

- Status: Accepted
- Date: 2026-09-26
- 覆す条件: [system.md の未実測の前提](../system.md#未実測の前提) 1・3 が成り立たないとき

## 決定

- **cache 層の書き方**: 宣言の `template`（`run`・`inputs`・`max_age_days`）で書く。重ね方は、`run` は init と同じく default → user → repo の順に並べて足し、`inputs` は和集合（secrets と同じ）、`max_age_days` は override
- **template の識別**: 出所ごとに作り、tag `sbxr-<小文字の名前>-<出所の hash>:<cache 層の hash>` で識別する。cache 層の hash は `run` と `inputs` の内容から決める。名前が同じでも出所が違えば別の template にする（同じ名前の別 repo の template を上書きしない。image の名前は小文字しか使えない）
- **作る時機**: create の中で作る。hash に合う template が無ければ build 用 VM で作ってから、本番の sandbox VM を作る
- **片付け**
  - 新しい hash の template を作ったら、同じ出所の旧いものを消す。消せなければ警告して残す
  - destroy では消さない
- **記録の置き場**: template の記録（出所・hash・作成日時）は、状態ディレクトリとは別の置き場に置く

## 根拠

- 目的は、destroy → create の作り直しを速くすること（#13）
- 出所で識別するのは、sandbox VM が名前 1 つに出所 1 つを求める（別出所は拒否）のに対し、template は destroy の後も残り、同じ名前の別 repo の template と並びうるため
- repo ごとに作れば、untrusted な repo の cache 層が他の repo の VM に入らない
- 明示のコマンドを足さずに済む。遅くなるのは、cache 層が変わった後の初回の create だけ

## 却下した代替案

- user 設定に書いて、全 repo で共通にする: repo 固有の tool を入れられない
- user 共通の template の上に repo の template を重ねる 2 段: 宣言・更新・片付けがすべて 2 段になる
- 明示のコマンド（`sbxr template build`）で作る。または create の自動作成と両方を持つ: 作り忘れると、create が cache 層を init として走らせる分岐が要る
- 利用者が明示のコマンド（prune）で消す。または自動の削除と両方を持つ: 片付けを利用者に委ねると、旧い template が溜まる
- destroy で消す: 作り直しで再利用するという目的に反する
