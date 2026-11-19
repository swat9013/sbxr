# 0021. 未 push の作業に、HEAD からだけ到達できる commit を含める

- Status: Accepted
- Date: 2026-11-19

## 決定

- 未 push の作業の commit は、local branch か HEAD から到達でき、origin の remote-tracking（VM 内で fetch した後のもの）から到達できない commit とする（decision/0003 の定義に HEAD を足す）
- 示すときは、branch に無い commit を「detached HEAD」として branch と並べる

## 根拠

- agent が detached HEAD で commit すると、どの local branch にも無い commit ができる。decision/0003 の定義では、destroy はこれに気付かずに撤去する

## 却下した代替案

- reflog からだけ到達できる commit も含める: 利用者が捨てた commit（reset・amend の前のもの）で毎回止まる
- VM 内の他の worktree の HEAD も含める: sbxr は VM 内の作業ツリーを 1 つしか作らず、用途が見えていない

## 見直し条件

置かない（定義の境界の決定で、実行の記録から測れる量が無い）
