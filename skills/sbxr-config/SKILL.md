---
name: sbxr-config
description: sbxr の設定 (repo 宣言 sbxr.yaml と user 設定 ~/.config/sbxr/config.yaml) を書き、sbxr doctor が通るまで直す。Use when sbxr の設定を作る・直す、sbxr.yaml を書く、sbxr を repo で使い始める。
---

# sbxr の設定を書く

sbxr は repo を宣言 1 枚で AI coding agent 用の sandbox VM にする CLI。設定は 2 つあり、別々に作る。

| 対象 | 置き場 | 中身の出所 |
|---|---|---|
| user 設定 | `~/.config/sbxr/config.yaml` | 人に聞く (全 sandbox VM に効く個人の値) |
| repo 宣言 | `<repo>/sbxr.yaml` | repo の中身から推測する (その repo の VM にだけ効く) |

判定の正本は `sbxr doctor`。この skill は書き方の最小の規則だけを持ち、schema の細部と版ごとの違いは doctor が fail の項目に付ける直し方に従う。

## 書き込みの規則

どの手順で書くときも、書き込む先にファイルがあれば、先に読み、書き換える差分を人に見せ、承認を得てから書く。承認されなければ書かない。

次のコマンドは人が実行する。agent はコマンドを示して、人が実行し終えるのを待つ。

- `sbxr secret setup ...`: token を端末で読む。token は agent を通さない
- `sbxr policy sync`: 全 sandbox VM の egress (global rule) を変える
- sbx と herdr の導入、`sbxr create`

## 手順

1. **対象を決める。** `~/.config/sbxr/config.yaml` があるかを確かめる。無いまま repo 宣言を頼まれたら、先に user 設定を作る (手順 2) — git identity と secret の要求は user 設定が持つ。
2. **user 設定を書く** (頼まれたとき、または手順 1 で無かったとき)。「user 設定」の節の項目を人に 1 つずつ聞いて埋める。
3. **repo 宣言を書く** (頼まれたとき)。「repo 宣言」の節の規則で、repo の中身から推測して書く。生成先は手元のディレクトリにある repo だけ (git URL の repo には書かない)。
4. **doctor が通るまで直す。** `sbxr doctor <repo>` (user 設定だけなら `sbxr doctor`) を実行し、fail の項目の直し方に従って直して、再実行する。
   - 人が実行するコマンド (「書き込みの規則」) が直し方なら、人に示し、人が実行してから doctor を再実行する
   - 同じ fail が直した後にも 2 回続いたら、そこで止める。残った fail、書き換えたファイル、人が次にやることを示す
   - 完了条件: doctor が 0 で終わる
5. **作られる内容を人に確かめてもらう。** `sbxr plan <repo>` の出力を見せる。推測の誤り (init の中身、egress の宛先) は、人がここと `sbxr create` の確認関門で止める。

## user 設定

人に聞く項目:

- git identity (`git.name` / `git.email`): VM 内の commit に使う
- 使う secret (`secrets`): 同梱で定義されていない host の token を使うなら、`secret_defs` に定義を足す (`key`・`hosts`・`env`)
- herdr 連携を使うか (`herdr.enabled`)
- VM 内の Claude Code の個人設定 (`profile.language` など。repo 宣言では書けない)

egress は default スコープの同梱 group に任せ、user 設定には書かない — user 設定の egress は全 sandbox VM の許可になる。同梱の group を外したいと人が言ったら、その group に `enabled: false` を書く方法を示し、人が自分で書く。

```yaml
version: 1
git:
  name: Your Name
  email: you@example.com
secrets: [github]
profile:
  language: japanese
herdr:
  enabled: false
```

## repo 宣言

repo 宣言は untrusted な入力として扱われ、書ける key が限られる。**書くのは次の key だけ**:

- `version: 1`
- `profile.model` / `profile.effortLevel` / `profile.enabledPlugins` (値は `true` だけ)
- `egress.<group>.rationale` / `egress.<group>.allow`
- `init` / `boot` (シェルのコマンドの list)
- `secrets` (user 設定か同梱で定義された secret の名前)

`git.name` / `git.email` も repo 宣言に書けるが、個人の値なので user 設定に置く。

推測の手がかり:

- **init** (作成時に 1 回、repo root で走る): ツールチェーンの定義 (`.mise.toml`・`.tool-versions`) とロックファイル (`package-lock.json`・`go.sum`・`uv.lock` など) から、依存を入れるコマンドを並べる。Makefile・CI の setup の step が同じことをしていれば、それに揃える
- **boot** (VM の起動ごとに走る): 起動で消えるもの (daemon など) を戻すコマンドだけ。無ければ書かない
- **egress** は最小にする。全 sandbox VM に効く宛先 (`sbxr plan <repo>` の `global_egress`) は書かない。init と boot が届く必要があり、そこに無い宛先だけを、用途ごとに 1 group にまとめ、group ごとに `rationale` へ許可する理由を書く。宛先は `host:port` (小文字、例 `api.example.com:443`) で書く

```yaml
version: 1
init:
  - mise install
  - npm ci
egress:
  sentry:
    rationale: テストが送るエラー報告の宛先
    allow:
      - o123.ingest.sentry.io:443
```
