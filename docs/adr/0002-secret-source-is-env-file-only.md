# secret の取得元は secret ファイルだけにし、注入は placeholder 方式を据え置く

Status: accepted (2026-09-25)

secret には「host 側で値をどこから読むか（取得元）」と「VM へどう渡すか（注入）」の 2 つの軸がある。旧実装は取得元が macOS keychain（`security`）固定で Linux では動かなかった。一方、注入は `sbx secret` と `set-custom` の placeholder 注入で、実値が VM に入らない安全側にあった。

取得元だけを `~/.config/sbxr/secrets.env`（dotenv、mode 0600 以外は拒否）に一本化し、注入は据え置く。`command:` による解決（keychain / 1Password 等）は要素を減らすために持たない。keychain を使いたい利用者は secret ファイルへ書き出す。

## Considered Options

- workspace 内の平文 env ファイル（streamingfast/sbox の `.sbox/env` 方式）: VM 内の agent が読め、commit 事故の余地がある。値を VM の環境変数へ入れるので placeholder 注入より後退する
- secret ファイル + `command:` 解決: macOS keychain の運用を残せるが、取得元が 2 種類になり、wizard の格納処理も分岐する

## 改訂（2026-09-26、#31）

設計ドキュメント（[docs/design/sbxr/](../design/sbxr/)）の決定で、repo を VM へ渡すときの扱いを次のように定める（[decision/0002](../design/sbxr/decision/0002-workspace-modes.md)）。

- 投入方式 copy は、ignored のファイルと、`.git/config` の認証にかかわる設定（URL の userinfo・credential・`http.extraHeader` など）を VM に持ち込まない。実値を VM に入れないという上の判断を、repo の持ち込みにも当てはめる
- 投入方式 mount では、host の作業ツリーの ignored のファイル（`.env` など）も VM から見える。利用者が create の flag で明示し、確認関門でそのことを示したときだけ許す
