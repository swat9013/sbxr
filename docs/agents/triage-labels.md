# Triage Labels

The skills speak in terms of five canonical triage roles. This file maps those roles to the actual label strings used in this repo's issue tracker.

| Label in mattpocock/skills | Label in our tracker | Meaning                                  |
| -------------------------- | -------------------- | ---------------------------------------- |
| `needs-triage`             | `needs-triage`       | Maintainer needs to evaluate this issue  |
| `needs-info`               | `needs-info`         | Waiting on reporter for more information |
| `ready-for-agent`          | `ready-for-agent`    | Fully specified, ready for an AFK agent  |
| `ready-for-human`          | `ready-for-human`    | Requires human implementation            |
| `wontfix`                  | `wontfix`            | Will not be actioned                     |

When a skill mentions a role (e.g. "apply the AFK-ready triage label"), use the corresponding label string from this table.

## Repo-specific roles

Roles this repo adds on top of the five canonical ones.

| Role              | Label in our tracker | Meaning                                                                 |
| ----------------- | -------------------- | ----------------------------------------------------------------------- |
| awaiting grilling | `need-grilling`      | Has open design questions; resolve them in a grilling session before `ready-for-agent` |

When a skill mentions the awaiting-grilling (深掘り待ち) role, apply `need-grilling`. Once grilling settles the questions, record the decisions as an issue comment, remove `need-grilling`, and re-review the issue before applying `ready-for-agent`.

Edit the right-hand column to match whatever vocabulary you actually use.
