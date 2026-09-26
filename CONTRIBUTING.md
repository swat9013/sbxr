# Contributing

sbxr は v0.1.0 を release 済み。語彙は [CONTEXT.md](./CONTEXT.md)、設計判断は [docs/adr/](./docs/adr/) を正本とする。

## セットアップ

- 必要なものは [README.md](./README.md#必要なもの) を参照
- clone ごとに secret 検査の pre-commit hook を有効化する（gitleaks が staged の内容を検査する）

  ```sh
  brew install pre-commit gitleaks
  pre-commit install
  ```

## branch・worktree 運用

- 作業は GitHub issue 単位で行う。default branch は `main`
- issue ごとに `main` から branch（worktree）を切り、PR で `main` へ入れる
- PR 本文に `Closes #<issue 番号>` を書き、merge で issue を閉じる

## gate（品質チェック）

- commit 前: pre-commit の gitleaks と gofmt
- PR / push: GitHub Actions で `go test ./...`・`golangci-lint`・`goreleaser check`
- PR: GitHub Actions が `docs/design/sbxr/decision/` の既存 file の変更・削除・改名を拒む（decision は不変）
- `v*` tag の push: release workflow が `go test ./...` の後に goreleaser で GitHub Release と `swat9013/homebrew-tap` の cask を出す（secret `HOMEBREW_TAP_GITHUB_TOKEN` が要る）

## commit・PR 規約

- commit message は Conventional Commits 形式で、subject は日本語で書く（例: `docs(adr): 0006 の前提が未実測の推論であることを明記する`）
- 設計判断を変える変更は、該当する ADR の追記・新規 ADR とあわせて出す

## issue の範囲

- 旧実装（dotfiles の `sbx-repo.sh` ほか）は動作の参考にとどめ、持ち込むのは処理の意味だけにする（ADR 0001）。旧名・旧 path・旧宣言の形は持ち込まない。移植する処理は issue に関数単位で書かれたものに限る
- issue の範囲外の変更が要ると気付いたら、その issue では実装しない。follow-up issue を切り、PR 本文に理由とあわせて列挙する
- 受け入れ条件は sbx stub 上の test で満たす。実 sbx での確認は、手順と結果を PR 本文に checklist で残す。create から destroy までの 1 周の手順は [docs/real-sbx-walkthrough-macos.md](./docs/real-sbx-walkthrough-macos.md)
- 実 sbx でしか確かめられず先へ進めないときは実装を止め、停止理由・そこまでの成果・人間が実行する確認手順を PR 本文に書いて返す
