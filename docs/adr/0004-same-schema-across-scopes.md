# 3 スコープで同じ schema を使い、全 VM 共通の egress は user 設定に置く

Status: accepted (2026-09-25)

default（CLI に同梱）・user（`~/.config/sbxr/config.yaml`）・repo（`sbxr.yaml`）の 3 スコープで同じ key 体系（`version` / `profile` / `git` / `egress` / `init` / `boot` / `secrets`）を使う。スコープごとの差は 1 つの制限表で定義する（例: repo の egress は sandbox スコープ rule になる、repo は base が固定した key を上書きできない）。先頭の `version: 1` で将来の schema 変更を CLI が検出できるようにする。

egress は sbxr 独自の形式で書き、実行基盤の rule へは adapter が変換する。宛先の置き場は「全 VM 共通 = user 設定（global rule）」「repo 固有 = repo 宣言（sandbox スコープ rule）」の 2 つで、汎用の宛先グループは default に同梱する。

旧実装は「user スコープに egress を書けない、全 VM 共通の宛先は別ファイル（`network.json`、sbx の rule 形式そのまま）」としていた。この規則の目的は置き場を 3 つに増やさないことで、1 ファイルにまとめても置き場は 2 つのまま保たれる。
