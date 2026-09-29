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

判定の正本は `sbxr doctor`。この skill は書き方の最小の規則だけを持ち、schema の細部と版ごとの違いは doctor の error (どの key が・なぜ駄目で・どう直すか) に従う。

## 手順

1. **対象を決める。** `sbxr doctor` を実行し、「user 設定」の項目を見る。user 設定が無い (`は無い` と出る) まま repo 宣言を頼まれたら、先に user 設定を作る (手順 2) — git identity と secret の要求は user 設定が持つ。
2. **user 設定を書く** (頼まれたとき、または手順 1 で無かったとき)。「user 設定」の節の項目を人に 1 つずつ聞いて埋める。
3. **repo 宣言を書く** (頼まれたとき)。「repo 宣言」の節の規則で、repo の中身から推測して書く。生成先は手元のディレクトリにある repo だけ (git URL の repo には書かない)。
4. **既存のファイルを守る。** 書き込む先にファイルがあれば、先に読み、書き換える差分を人に見せ、承認を得てから書く。承認されなければ書かない。
5. **doctor が通るまで直す。** `sbxr doctor <repo>` (user 設定だけなら `sbxr doctor`) を実行し、fail の `直し方:` に従って直して、再実行する。
   - 人の手が要る fail は、コマンドを示して人に実行してもらう: secret の値 (`sbxr secret setup ...`。token を agent に渡さない)、global rule (`sbxr policy sync`。全 sandbox VM の egress を変える)、sbx・herdr の導入
   - 完了条件: doctor が 0 で終わる。または残る fail がすべて人の手が要るもので、そのコマンドを人に示した
6. **作られる内容を人に確かめてもらう。** `sbxr plan <repo>` の出力を見せる。推測の誤り (init の中身、egress の宛先) は人がここと `sbxr create` の確認関門で止める。

## user 設定

人に聞く項目:

- git identity (`git.name` / `git.email`): VM 内の commit に使う
- 使う secret (`secrets`): 同梱の定義は `github` だけ。他の host の token を使うなら、`secret_defs` に定義を足す (`key`・`hosts`・`env`)
- herdr 連携を使うか (`herdr.enabled`)
- VM 内の Claude Code の個人設定 (`profile.language` など。repo 宣言では書けない)

egress は書かない。default スコープの同梱 group (`github`・`mise-tools`・`docker-registry`・`ubuntu-apt`・`cert-validation`) に任せる — user 設定の egress は全 sandbox VM の許可になる。人が同梱の group を外したいと言ったときだけ、その group に `enabled: false` を書く。

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

推測の手がかり:

- **init** (作成時に 1 回、repo root で走る): ツールチェーンの定義 (`.mise.toml`・`.tool-versions`) とロックファイル (`package-lock.json`・`go.sum`・`uv.lock` など) から、依存を入れるコマンドを並べる。Makefile・CI の setup の step が同じことをしていれば、それに揃える
- **boot** (VM の起動ごとに走る): 起動で消えるもの (daemon など) を戻すコマンドだけ。無ければ書かない
- **egress** は最小にする。全 sandbox VM に効く宛先 (`sbxr plan <repo>` の `global_egress` に並ぶもの) は書かない。init と boot が届く必要があり、そこに無い宛先だけを、用途ごとに 1 group にまとめ、group ごとに `rationale` へ許可する理由を書く。宛先は `host:port` (小文字、例 `api.example.com:443`) で書く

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
