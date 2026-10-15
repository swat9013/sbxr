# Triage Labels

The skills speak in terms of five canonical triage roles. This file maps those roles to the actual label strings used in this repo's issue tracker.

| Label in mattpocock/skills | Label in our tracker | Meaning                                  |
| -------------------------- | -------------------- | ---------------------------------------- |
| `needs-triage`             | `needs-triage`       | Maintainer needs to evaluate this issue  |
| `needs-info`               | `needs-info`         | Waiting on reporter for more information |
| `ready-for-agent`          | `ready-for-agent`    | Fully specified, ready for an AFK agent  |
| `ready-for-human`          | `ready-for-human`    | Blocked on a step only a human can do (implementation, host-only measurement, credentialed operation) |
| `wontfix`                  | `wontfix`            | Will not be actioned                     |

When a skill mentions a role (e.g. "apply the AFK-ready triage label"), use the corresponding label string from this table.

## Repo-specific roles

Roles this repo adds on top of the five canonical ones.

| Role              | Label in our tracker | Meaning                                                                 |
| ----------------- | -------------------- | ----------------------------------------------------------------------- |
| awaiting grilling | `need-grilling`      | Has open design questions; resolve them in a grilling session before `ready-for-agent` |
| planned split parent | `tracking-issue` | Parent of a planned split into child issues; not implemented directly |

When a skill mentions the awaiting-grilling (深掘り待ち) role, apply `need-grilling`. Once grilling settles the questions, record the decisions as an issue comment, remove `need-grilling`, and re-review the issue before applying `ready-for-agent`.

Edit the right-hand column to match whatever vocabulary you actually use.

## agent が進められない issue の行き先

triage は、止まっている理由で label か close かを選ぶ。

- 人の手でしか進まない作業（host でしか実行できない実測、人の資格情報が要る操作など）で止まる issue には `ready-for-human` を付ける。triage のコメントに、人が行う手順と、終えたら `needs-triage` に戻すことを書く。作業の結果で次の行き先が変わるので、triage に判定し直させる
- 設計の論点が開いて止まる issue には `need-grilling` を付ける。grilling で決まるのは設計判断だけで、人の作業は進まない
- 実装することが残っていない監視の issue（外部の条件が揃うまで何もしないと決めた調査結果など）は、`wontfix` を付けて閉じる。open のままだと着手できる issue の一覧に混ざり、`wontfix` で閉じれば `docs/agents/issue-tracker.md`「follow-up issue の起票」の重複探しが見つける。監視の条件は `docs/design/sbxr/system.md` の「未実測の前提」へ移す follow-up issue を、同じ「follow-up issue の起票」に従って起票する
