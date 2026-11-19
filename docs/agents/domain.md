# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`CONTEXT.md`** at the repo root, or
- **`CONTEXT-MAP.md`** at the repo root if it exists: it points at one `CONTEXT.md` per context. Read each one relevant to the topic.
- **`docs/adr/`**: read ADRs that touch the area you're about to work in. In multi-context repos, also check `src/<context>/docs/adr/` for context-scoped decisions.

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The `/domain-modeling` skill (reached via `/grill-with-docs` and `/improve-codebase-architecture`) creates them lazily when terms or decisions actually get resolved.

## File structure

Single-context repo (most repos):

```
/
├── CONTEXT.md
├── docs/adr/
│   ├── 0001-event-sourced-orders.md
│   └── 0002-postgres-for-write-model.md
└── src/
```

Multi-context repo (presence of `CONTEXT-MAP.md` at the root):

```
/
├── CONTEXT-MAP.md
├── docs/adr/                          ← system-wide decisions
└── src/
    ├── ordering/
    │   ├── CONTEXT.md
    │   └── docs/adr/                  ← context-specific decisions
    └── billing/
        ├── CONTEXT.md
        └── docs/adr/
```

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `CONTEXT.md`. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal: either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/domain-modeling`).

用語集の語を改名する変更と、`_Avoid_` に語を足す変更では、避ける語（旧称と `_Avoid_` に足した語）を置き換える対象に、comment・CLI の文面・文書に加えて、その概念を名指す識別子（型・関数・変数・test の名前）と test の失敗文を含める。`docs/design/sbxr/decision/` と `docs/adr/` の既存 file は不変なので置き換えない。

避ける語を英訳や略語で綴った識別子も、避ける語として数える（例: 確認関門 → `Gate`）。避ける語の一部でも、用語集の定義文が今も使う語をその意味で使う識別子は残す（例: 避けるのは「placeholder 注入」で、credential injection の定義文が使う placeholder は残す）。

識別子を対象から外すと、comment は改めた語、名前は避ける語という行が残り、用語集の語で grep しても型や関数に当たらない。

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders), but worth reopening because…_

## 設計 doc の置き場

| 置き場 | 適用範囲 | 索引 |
|---|---|---|
| `docs/design/sbxr/` | `cmd/**`・`internal/**`・`skills/**` | `docs/design/sbxr/README.md` |
