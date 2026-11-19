## 開発フロー

作業を始める前に `CONTRIBUTING.md` を読み、branch・worktree 運用と commit・PR 規約に従う。

repo の追跡対象 file を変更する作業は、`main` の checkout で始まったセッションなら最初の編集の前に EnterWorktree tool で worktree に入り、その中だけで編集・commit する（既に worktree で始まったセッションはそのまま続ける）。tool が付ける branch 名 `worktree-<name>` は、入った直後に `git branch -m` で `<type>/<issue 番号>-<slug>`（例: `fix/34-vm-vanished`。issue が無ければ `<type>/<slug>`）へ直す。完了したら push して PR を作るところまで確認なしで進める。`main` の checkout は並行する worktree の起点なので、常に clean に保つ。

設計ドキュメント（`docs/design/sbxr/README.md` の索引が挙げる正本）に書かれた構造を変える・足す実装は、実装の commit より前に次を済ませる。置き場は、実装と同じ PR の中の実装より前の commit か、実装の PR より先に merge する設計だけの PR にする。設計だけの PR は、深掘りで決めた設計を実装する側が `main` から読めるようにするときに使う。本文には `Closes` ではなく `Refs #<issue 番号>` を書き、PR を作った時点で止める（issue は実装の PR が閉じる）。

- `docs/design/sbxr/decision/` へ決定ごとに 1 file を足す。既存の file は残す（README の索引のとおり不変）
- 索引が挙げる正本の file を直す

実装を先にすると、根拠と却下した代替案を後から復元できない。repo 全体の設計判断を変えるときの ADR の扱いは `CONTRIBUTING.md` に従う。

## Agent skills

### Issue tracker

Issue は GitHub Issues (swat9013/sbxr) で管理し、`gh` CLI で操作する。See `docs/agents/issue-tracker.md`.

### Triage labels

issue に label を付ける・triage で issue の行き先を決める前に、`docs/agents/triage-labels.md` を読む。

### Domain docs

single-context: root の `CONTEXT.md` と `docs/adr/`。codebase を調べる前と、用語集の語を改名する・`_Avoid_` に語を足す・避ける語（旧称と `_Avoid_` の語）を置き換える前に、`docs/agents/domain.md` を読む。実装に入る前に、設計ドキュメントの索引 `docs/design/sbxr/README.md` も読む。

## Worktree 方式

- git diff に差分が出る作業は git worktree 上で行う (読み取りだけの作業は除く)
- 差分の出る作業を subagent に任せるときは、Agent tool に `isolation: "worktree"` を渡す
