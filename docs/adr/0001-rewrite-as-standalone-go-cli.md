# 別 repo の Go CLI として作り直し、旧実装との互換は持たない

Status: accepted (2026-09-25)

sbxr は、個人 dotfiles 内の bash + Python 実装（`sbx-repo.sh` と `sandbox-config.py` ほか）を、独立した public repo の Go CLI（cobra）として作り直したものである。Go を選んだ主因は、子プロセスの stdin が既定で null device になり（`sbx exec` が stdin を読み切る問題を言語の既定で防げる）、資材と CLI の版を 1 つの binary に固定でき、shell completion が標準で付くこと。

旧実装の宣言ファイル（`.sandbox.yaml`、`~/.config/sbx/` 一式）とは互換を持たず、移行手段（adopt）も作らない。利用者が作者 1 人で稼働中の sandbox VM が無く、互換を捨てれば schema を 3 スコープ共通に設計し直せるため。

保つのは宣言の**意味**である: merge 規則、repo 宣言を untrusted として扱う制限、egress の 2 つの置き場、boot の再生、確認関門、git URL 入力を確認なしで通したときに repo 由来の egress を落とす挙動。受け入れテストはこの意味の側を検査する。

## Considered Options

- Python（uv tool）: 旧実装の merge ロジックと pytest を持ち込めて移植は最小だが、subprocess が既定で stdin を継承するため `stdin=DEVNULL` を必ず通す規律が要る
- 旧実装のまま別 repo へ移す: 移すだけなら足りるが、bash + Python の 2 言語構成と jq / awk の文字列処理が残る
