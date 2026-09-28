# Contributing

語彙は [CONTEXT.md](./CONTEXT.md)、設計判断は [docs/adr/](./docs/adr/) を正本とする。

## セットアップ

- 必要なものは [README.md](./README.md#必要なもの) を参照
- clone ごとに pre-commit hook を有効化する（gitleaks が staged の内容を検査し、golangci-lint が CI と同じ `.golangci.yml` で整形する）

  ```sh
  brew install pre-commit gitleaks golangci-lint
  pre-commit install
  ```

## branch・worktree 運用

- 作業は GitHub issue 単位で行う。default branch は `main`
- issue ごとに `main` から branch（worktree）を切り、PR で `main` へ入れる
- PR 本文に `Closes #<issue 番号>` を書き、merge で issue を閉じる

## gate（品質チェック）

- commit 前: pre-commit の gitleaks と `golangci-lint fmt`（gofmt・goimports）
- PR と `main` への push: CI workflow（`.github/workflows/ci.yml`）が次を走らせる。`main` への merge には集約 job `ci-ok` の通過が要る（ruleset）
  - test（ubuntu・macOS）: `go mod tidy -diff`・`go mod verify`・`go test -race -shuffle=on ./...`
  - lint: `golangci-lint`（`.golangci.yml`。整形の崩れもここで落ちる）
  - vulncheck: `govulncheck`（週 1 回の schedule でも走る）
  - goreleaser-check: `goreleaser check`
  - decision-immutable（PR だけ）: `docs/design/sbxr/decision/` の既存 file の変更・削除・改名を拒む（decision は不変）
- `v*` tag の push: release workflow が CI workflow を呼び、通った後に goreleaser で GitHub Release と `swat9013/homebrew-tap` の cask を出す（secret `HOMEBREW_TAP_GITHUB_TOKEN` が要る）
- 依存と actions の更新: Dependabot が週 1 回 PR を出す。actions は commit SHA で固定しているので、更新は Dependabot に任せる

## commit・PR 規約

- commit message は Conventional Commits 形式で、subject は日本語で書く（例: `docs(adr): 0006 の前提が未実測の推論であることを明記する`）。Dependabot の commit（`chore(deps)`・`ci(deps)`）は例外で、生成された英語の subject のままにする
- 設計判断を変える変更は、該当する ADR の追記・新規 ADR とあわせて出す

## issue の範囲

- 旧実装（dotfiles の `sbx-repo.sh` ほか）は動作の参考にとどめ、持ち込むのは処理の意味だけにする（ADR 0001）。旧名・旧 path・旧宣言の形は持ち込まない。移植する処理は issue に関数単位で書かれたものに限る
- issue の範囲外の変更が要ると気付いたら、その issue では実装しない。follow-up issue を切り、PR 本文に理由とあわせて列挙する
- 受け入れ条件は、実 sbx を使わない test（sbx stub か in-memory の Runtime adapter）で満たす。実 sbx での確認は、手順と結果を PR 本文に checklist で残す。create から destroy までの 1 周の手順は [docs/real-sbx-walkthrough-macos.md](./docs/real-sbx-walkthrough-macos.md)
- 実 sbx でしか確かめられず先へ進めないときは実装を止め、停止理由・そこまでの成果・人間が実行する確認手順を PR 本文に書いて返す
