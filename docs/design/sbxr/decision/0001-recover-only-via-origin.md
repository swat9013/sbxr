# 0001. 回収は VM 内から origin への push に限り、host 側 repo へ取り込む経路を持たない

- Status: Accepted
- Date: 2026-09-26

## 決定

sbxr は `fetch` を持たない。clone・copy の sandbox VM の作業は、VM 内から origin へ push して残す。host 側 repo へ戻す手段（export・sync のようなコマンド）は将来の検討に回す。「回収」の意味をこれに合わせて再定義する（CONTEXT.md）。

## 根拠

- host 側 repo への取り込みには、次がすべて要る
  - VM の稼働
  - git URL の VM での取り込み先。cache clone は destroy で消える
  - 取り込み先の ref の設計。remote-tracking・専用の ref・local branch のどれにするか
  - sbx rm が `sandbox-<name>` remote を消すかの実測
- 仕組みが複雑になる割に、「VM が消えても作業が残る」という目的は origin への push で満たせる

## 却下した代替案

- `sbxr fetch` で `refs/sbxr/<name>/*` へ取り込む: remote が消えても残り、branch の一覧も汚さない。ただし、上の複雑さを抱える
- `sbxr fetch` で remote-tracking まで取り込む（README の現行の案内どおり）: sbx rm が remote を消すと、取り込んだ commit に到達できなくなりうる
- `sbxr fetch` で local branch `sbxr/<name>/<branch>` へ取り込む: `git branch` の一覧を汚す。複雑さは上と同じ
