# Contributing

sbxr は設計段階（v0.1 未リリース）。語彙は [CONTEXT.md](./CONTEXT.md)、設計判断は [docs/adr/](./docs/adr/) を正本とする。

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

## commit・PR 規約

- commit message は Conventional Commits 形式で、subject は日本語で書く（例: `docs(adr): 0006 の前提が未実測の推論であることを明記する`）
- 設計判断を変える変更は、該当する ADR の追記・新規 ADR とあわせて出す
