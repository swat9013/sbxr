## 開発フロー

作業を始める前に `CONTRIBUTING.md` を読み、branch・worktree 運用と commit・PR 規約に従う。

設計ドキュメント（`docs/design/sbxr/README.md` の索引が挙げる正本）に書かれた構造を変える・足す実装は、同じ PR の中で、実装の commit より前に次を済ませる。

- `docs/design/sbxr/decision/` へ決定ごとに 1 file を足す。既存の file は残す（README の索引のとおり不変）
- 索引が挙げる正本の file を直す

実装を先にすると、根拠と却下した代替案を後から復元できない。repo 全体の設計判断を変えるときの ADR の扱いは `CONTRIBUTING.md` に従う。

## Agent skills

### Issue tracker

Issue は GitHub Issues (swat9013/sbxr) で管理し、`gh` CLI で操作する。See `docs/agents/issue-tracker.md`.

### Triage labels

デフォルトの 5 ラベル (needs-triage / needs-info / ready-for-agent / ready-for-human / wontfix) に、深掘り待ちの need-grilling を足して使う。See `docs/agents/triage-labels.md`.

### Domain docs

single-context: root の `CONTEXT.md` と `docs/adr/`。See `docs/agents/domain.md`. 実装に入る前に、設計ドキュメントの索引 `docs/design/sbxr/README.md` も読む。
