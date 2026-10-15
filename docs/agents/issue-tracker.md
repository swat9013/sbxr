# Issue tracker: GitHub

Issues and specs for this repo live as GitHub issues. Use the `gh` CLI for all operations.

## Conventions

- **Create an issue**: `gh issue create --title "..." --body "..."`. Use a heredoc for multi-line bodies.
- **Read an issue**: `gh issue view <number> --comments`, filtering comments by `jq` and also fetching labels.
- **List issues**: `gh issue list --state open --json number,title,body,labels,comments --jq '[.[] | {number, title, body, labels: [.labels[].name], comments: [.comments[].body]}]'` with appropriate `--label` and `--state` filters.
- **Comment on an issue**: `gh issue comment <number> --body "..."`
- **Apply / remove labels**: `gh issue edit <number> --add-label "..."` / `--remove-label "..."`
- **Close**: `gh issue close <number> --comment "..."`

Infer the repo from `git remote -v`; `gh` does this automatically when run inside a clone.

## Pull requests as a triage surface

**PRs as a request surface: no.** _(Set to `yes` if this repo treats external PRs as feature requests; `/triage` reads this flag.)_

When set to `yes`, PRs run through the same labels and states as issues, using the `gh pr` equivalents:

- **Read a PR**: `gh pr view <number> --comments` and `gh pr diff <number>` for the diff.
- **List external PRs for triage**: `gh pr list --state open --json number,title,body,labels,author,authorAssociation,comments` then keep only `authorAssociation` of `CONTRIBUTOR`, `FIRST_TIME_CONTRIBUTOR`, or `NONE` (drop `OWNER`/`MEMBER`/`COLLABORATOR`).
- **Comment / label / close**: `gh pr comment`, `gh pr edit --add-label`/`--remove-label`, `gh pr close`.

GitHub shares one number space across issues and PRs, so a bare `#42` may be either: resolve with `gh pr view 42` and fall back to `gh issue view 42`.

## When a skill says "publish to the issue tracker"

Create a GitHub issue.

## When a skill says "fetch the relevant ticket"

Run `gh issue view <number> --comments`.

## Wayfinding operations

Used by `/wayfinder`. The **map** is a single issue with **child** issues as tickets.

- **Map**: a single issue labelled `wayfinder:map`, holding the Notes / Decisions-so-far / Fog body. `gh issue create --label wayfinder:map`.
- **Child ticket**: an issue linked to the map as a GitHub sub-issue (`gh api` on the sub-issues endpoint). Where sub-issues aren't enabled, add the child to a task list in the map body and put `Part of #<map>` at the top of the child body. Labels: `wayfinder:<type>` (`research`/`prototype`/`grilling`/`task`). Once claimed, the ticket is assigned to the driving dev.
- **Blocking**: GitHub's **native issue dependencies**, the canonical, UI-visible representation. Add an edge with `gh api --method POST repos/<owner>/<repo>/issues/<child>/dependencies/blocked_by -F issue_id=<blocker-db-id>`, where `<blocker-db-id>` is the blocker's numeric **database id** (`gh api repos/<owner>/<repo>/issues/<n> --jq .id`, _not_ the `#number` or `node_id`). GitHub reports `issue_dependencies_summary.blocked_by` (open blockers only, the live gate). Where dependencies aren't available, fall back to a `Blocked by: #<n>, #<n>` line at the top of the child body. A ticket is unblocked when every blocker is closed.
- **Frontier query**: list the map's open children (`gh issue list --state open`, scoped to the map's sub-issues / task list), drop any with an open blocker (`issue_dependencies_summary.blocked_by > 0`, or an open issue in the `Blocked by` line) or an assignee; first in map order wins.
- **Claim**: `gh issue edit <n> --add-assignee @me`, the session's first write.
- **Resolve**: `gh issue comment <n> --body "<answer>"`, then `gh issue close <n>`, then append a context pointer (gist + link) to the map's Decisions-so-far.

## follow-up issue の起票

作業の中で見つけた範囲外の欠陥・指摘を issue にするときは、次に従う。

- **変更した単位**: 変更した file と一緒に保守・レビューされるまとまり。skill なら skill のディレクトリ、それ以外は変更した file とその test
- **直す**: この CL の変更が生んだものと、単位の中で見つけたもの (レビューの区分を問わない)。ただし 1 CL で直せない大きさのものと `[上限超過]` は起票する (上限は、修正の差分を再レビュー 1 巡で見切れる量に保つためにある)。依頼や issue 本文が範囲を狭めているときはそれに従う
- **単位の外**: 欠陥 (レビューの Act on・Consider を含む。区分の語はレビューの出力のもの) は起票し、それ以外 (Noted・欠陥を伴わない改善) は起票せず CL の説明文に 1 行残す
- 判定条件が同じで対象だけ違う指摘は 1 件に束ね、対象を本文の checklist に並べる
- 起票の前に、同じ指摘の issue を、open なものと、`docs/agents/triage-labels.md` の表で `wontfix` の役を持つ label で close したものから、題名と変更対象の file path の 2 通りで探す。当たった issue は本文を読み、判定条件と対象が同じかで同じ指摘かを決める。同じ指摘の issue があれば起票せず、open なら発端の issue と CL の番号があればコメントで足す
- 起票した issue は、発端の issue (作業が担当している issue) の子にする。親子は tracker の親子の機能で付け、本書に「follow-up issue の深さ」節があれば、その測り方が辿れる形にする (測り方が `github-sub-issue` なら sub-issue にし、本文の行で代えない)。issue を介さず人が始めた作業なら親を付けない — 人が起点の作業は agent の起票の連鎖ではない
- 本文に `## 範囲` の節を置き、変更してよい範囲を、issue が直す file の「変更した単位」で書く。狭めるなら理由を書く。あわせて「範囲外で見つけた欠陥・指摘は `docs/agents/issue-tracker.md`「follow-up issue の起票」に従って扱う」を書く
- 付ける label は triage 待ち (`docs/agents/triage-labels.md` の表で `needs-triage` の役を持つ label) だけにする。着手可は triage が付ける。本書に「follow-up issue の深さ」節があれば、triage はそこで連鎖の深さを測ってから付ける

## follow-up issue の深さ

- 測り方: github-sub-issue
- 上限: 1
- 計画的な分解の親の label: tracking-issue
