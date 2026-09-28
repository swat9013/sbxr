# Docker Sandbox Kit spec（v3）が sbxr に与える影響

調査日: 2026-09-28。対象は、gihyo.jp の記事「Docker Sandbox Kit spec」が紹介する Docker Sandbox Kit Specification v3。記事は二次情報なので、各主張は一次情報で確かめた。一次情報は、spec の repo（commit `b1c53cb`、2026-09-26）、sbx の release notes、Docker Docs の 3 つ。

## 結論

**今すぐ変える必要のある箇所は無い。** sbxr が埋め込んでいる kit は v2（`schemaVersion: "2"`）で、env 定義は built-in の `agent: claude` を使う。sbx v0.45.0 の release notes と Docker Docs は、v2 kit と built-in agent を引き続きサポートすると明言している。sbxr の実測（sbx v0.45.1）は、v3 に対応した sbx の上で v2 の経路が動くことを既に確かめている。

**簡略化できる候補はあるが、どれも v3 への全面移行が前提になる。** v3 の kit は v1・v2 の kit と同じ sandbox に混ぜられない。そのため、1 箇所だけ v3 の機能で置き換えることはできない。置き換えるには次の 3 つを同時に行う。

- workload（`agent: claude` の代わりの v3 workload）を選ぶ
- `sbxr-boot` と `sbxr-herdr` を v3 に書き換える
- create の経路で kit を build する

さらに、置き換えの多くは ADR 0002・0006 の不変条件と衝突する。今の段階で「肩代わりさせて削れる」と言える項目は無い。

### 重要度順の一覧

| # | 分類 | 項目 | 確度 | 推奨 |
|---|---|---|---|---|
| 1 | 将来リスク（監視） | v2 kit と built-in `agent: claude` への依存。v3 と混ぜられないので、移行は全面になる | 確認済み | 今は何もしない。v2 の非推奨化を監視する。移行するときは ADR を 1 本足す |
| 2 | 将来リスク（監視） | kit startup の完了判定が v2 runtime の dispatcher の log の文面に依存している | 確認済み（依存の事実）／推測（v3 で変わるか） | v3 へ移るときに再実測する |
| 3 | 採用するなら ADR 改訂が要る | secret の注入を `credential@1` と env 定義の `bindings:` に置き換える | 確認済み（衝突の事実） | 採らない。採るなら ADR 0002・0006 を改訂する |
| 4 | 簡略化の候補（未確認） | sandbox スコープ rule を kit の `network-policy@1` に置き換え、作成後に rule を足す手順をなくす | 推測 | 実 sbx で確かめるまで採らない |
| 5 | 簡略化の候補（未確認） | boot の再生・init・materialize を `lifecycle@1` に寄せる | 推測（一部は不可と確認済み） | 採らない |
| 6 | 参考（spec と sbxr の一致） | 注入先 host が egress で許可されていることを要求する規則は、spec の cross-entry 検証と同じ | 確認済み | 何もしない（ADR 0003 の妥当性の裏付け） |
| 7 | 影響なし | 状態ディレクトリ（ADR 0006）、env 定義の schema、global rule の収束（ADR 0008）、sbx v0.45.0 の breaking changes | 確認済み | 何もしない |

## 前提: spec が決める範囲と決めない範囲

- spec が決めるのは **kit** の descriptor（`schemaVersion: "3"`）と、それを載せる OCI image の形だけである。sbx の **env 定義**（`sbxenv.yaml`。`schemaVersion: "1"`）は spec の外にある。spec の repo 自身の `sbxenv.yaml` も、冒頭で次のように書いている。「Its schemaVersion belongs to sbx environments; the Kit descriptors under examples/ use the separate Kit v3 grammar.」（[sbxenv.yaml](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/sbxenv.yaml)）
- runtime 側の状態の持ち方も、spec は決めない。SPEC-v3 §10 の最後の段落には「Local handles, state keying, and lock formats are runtime concerns outside this specification」とある（[SPEC-v3.md §10](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/docs/spec/SPEC-v3.md#10-the-oci-layout)）
- spec は experimental である。README には「This specification is experimental. … A final version is targeted for Q4 2026」とある（[README.md](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/README.md)）。変更は capability の版（`@1` → `@2`）で足していく方針で、`schemaVersion` を上げるのは最後の手段とされる（[RELEASES.md](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/RELEASES.md)）
- v3 は v2 の `spec.yaml` の後継である。SPEC-v3 の冒頭には「the v2 `spec.yaml` grammar this format succeeds」とある（[SPEC-v3.md](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/docs/spec/SPEC-v3.md)）

## sbxr が sbx に置いている前提

| 前提 | repo の箇所 |
|---|---|
| env 定義 `schemaVersion: "1"`・`agent: claude`・`workspace.clone: true`・`kits[].source: ./kits/<name>` | `internal/runtime/sbx_sandbox.go:26-44`, `:85-92` |
| 埋め込み kit は v2（`schemaVersion: "2"`・`kind: mixin`・`setup.startup`・`args`・`${{ kit.args.version }}`） | `internal/assets/kits/sbxr-boot/spec.yaml:4-18`, `internal/assets/kits/sbxr-herdr/spec.yaml:3-58` |
| `sbx env create --auto-approve <dir>` と `sbx env rm --force <dir>` | `internal/runtime/sbx.go:152-162` |
| sandbox スコープの secret を作成前に置く（`sbx secret set <service> --sandbox`、`sbx secret set-custom --sandbox --host --env`） | `internal/runtime/sbx.go:99-111`, `internal/runtime/sbx_sandbox.go:132-139` |
| sandbox スコープ rule は作成後に足す（`sbx policy allow network --sandbox`） | `internal/runtime/sbx.go:170-174`, `internal/runtime/sbx_sandbox.go:143-147` |
| global rule の収束（`sbx policy ls --json`・`allow network`・`rm network --id`） | `internal/runtime/sbx.go:56-94`, `internal/egress/sync.go` |
| VM の状態は `sbx ls --json` の `status`。`stopped` 以外は稼働中と読む | `internal/runtime/sbx.go:114-149` |
| kit startup の完了を `/var/log/sbx-kit-startup.log` の dispatcher の行で判定する | `internal/runtime/sbx_sandbox.go:195-257` |
| VM 内のファイルの読み書きは `sbx exec -i` で行う（`sbx cp` を使わない） | `internal/runtime/sbx.go:176-220` |
| settings.json は sbx が置いた初期値に再帰的に merge する | `internal/sandbox/materialize.go:160-183` |

## 各項目

### 1. v2 kit と built-in agent への依存（将来リスク）

- **repo**: `internal/assets/kits/sbxr-boot/spec.yaml:4`、`internal/assets/kits/sbxr-herdr/spec.yaml:3`（`schemaVersion: "2"`）。`internal/runtime/sbx_sandbox.go:87`（`Agent: "claude"`）
- **根拠**
  - sbx v0.45.0 の release notes には次の 2 文がある。「V2 kits remain supported for built-in agents and existing customizations. V3 workloads and mixins must be used together; they can't be combined with v1 or v2 kits.」（[sbx-releases v0.45.0](https://github.com/docker/sbx-releases/releases/tag/v0.45.0)、2026-09-21）
  - Docker Docs には「V2 kits remain supported.」「Built-in shortcuts such as `claude` and `codex` select v2 kits and still work with v2 mixins.」とある。移行については「Changing `schemaVersion` alone doesn't convert a kit」と書き、「select a v3 workload, convert or replace its mixins, and create a separate sandbox with a different `--name`」を求めている（[Kits v2](https://docs.docker.com/ai/sandboxes/customize/kits-v2/)）
  - sbxr の実測は sbx v0.45.1（Latest。v3 対応の v0.45.0 より後）で行った（ADR 0006 の実測、system.md の実測）
- **影響**
  - 今は壊れていない
  - v2 の非推奨化はまだ告知されていない。ただし spec は v3 を v2 の後継と位置付けている
  - 移行は部分的にできない。v3 の kit を 1 つでも使うと、`agent: claude`（v2）をやめて v3 の workload に替える必要がある。つまり agent runtime profile の土台（sbx が置く settings.json の初期値など）も替わる
  - 移行の手順は、名前を替えて sandbox を作り直すことである。既存の VM を移行する経路は無い
- **確度**: 確認済み
- **推奨**
  - 今は何もしない
  - sbx の release notes で v2 の deprecation を監視する
  - 移行を決めるときは、次の 3 つを 1 本の ADR にまとめる: v3 workload の選定、kit の build の経路、既存の VM の扱い。ADR 0005 と decision/0009 の「env 定義と kit は adapter の内側」という切り方は、この移行を adapter の中に閉じ込める形になっている

### 2. kit startup の完了判定が v2 runtime の log の文面に依存している（将来リスク）

- **repo**: `internal/runtime/sbx_sandbox.go:195-257`
  - `/etc/durable-startup.d/run.sh` の dispatcher が出す `=== dispatcher run`・`=== dispatcher complete ===`・`fail /etc/durable-startup.d/… exit=` の行を読む
- **根拠**
  - `lifecycle@1` は startup hook を「every boot」で走らせる義務を決めている。一方で、log の場所と文面は決めていない（[lifecycle@1.md](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/docs/spec/capabilities/com.docker.sandbox/lifecycle@1.md)）
  - spec の移行 skill と README は、v3 の kit でも log の path として `cat /var/log/sbx-kit-startup.log` を案内している（[migrate-kit-to-v3/SKILL.md](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/skills/migrate-kit-to-v3/SKILL.md)）。path は同じだが、中の文面が同じかは書かれていない
- **影響**
  - v2 のままなら影響は無い
  - v3 へ移ると、完了判定が読む文面の前提が崩れうる。崩れた場合、herdr を有効にした create は完了を読めず、300 秒待った後に失敗する
- **確度**: 依存の事実は確認済み。v3 で文面が変わるかは推測
- **推奨**: v3 へ移るときの確認項目に入れる（system.md の「未実測の前提」に並べる形）

### 3. secret の注入を `credential@1` と `bindings:` に置き換える案（採用するなら ADR 改訂が要る）

- **repo**: `internal/runtime/sbx.go:96-111`、`internal/secret/wiring.go`、ADR 0002、ADR 0003、ADR 0006
- **根拠**
  - `credential@1` は注入を proxy に任せる方式で、実値は container に入らない（`proxyManaged`、sentinel）。これは sbxr の placeholder 注入と同じ性質である。一方で取得元について、spec は次のように決めている（[credential@1.md](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/docs/spec/capabilities/com.docker.sandbox/credential@1.md)）
    - 「The host's credential store is the sole source」
    - 「MUST resolve the credential from the host-side store keyed by `service`」
  - spec の repo の `sbxenv.yaml` によると、env 定義の `bindings:` は保存済みの credential の利用を承認するだけで、値を作らない。さらに次の性質を持つ（[sbxenv.yaml](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/sbxenv.yaml)）
    - 「These merge into ~/.config/sbx/credentials.yaml and survive `sbx env rm` unless it is given --prune-bindings」
- **影響**: 置き換えると、sbxr の 2 つの不変条件と衝突する
  - ADR 0002 は、secret の取得元を `~/.config/sbxr/secrets.env` だけにしている。`credential@1` の取得元は sbx の host 側の store（`sbx secret` の global）になり、取得元が 2 つに分かれる
  - ADR 0006 は、sandbox スコープの secret が `sbx env rm` で消えることに依っている。`bindings:` は既定では `env rm` の後も残るので、destroy の後に承認が残る
  - もう 1 点ある。v3 の kit を使うので、項目 1 の全面移行も前提になる
- **確度**: 衝突の事実は確認済み。sandbox スコープの secret（`sbx secret set --sandbox`）を `credential@1` の取得元にできるかは確かめていない（推測）
- **推奨**: 簡略化として扱わない。採るなら ADR 0002 と ADR 0006 の改訂が要る。今の sbxr の方式（sandbox スコープの secret）は v0.45.x でも動いている

### 4. sandbox スコープ rule を kit の `network-policy@1` に置き換える案（簡略化の候補・未確認）

- **repo**
  - `internal/runtime/sbx_sandbox.go:143-147`（作成後に rule を足し、失敗したら `CreatedStepSandboxEgress` の `CreatedError` を返す）
  - `internal/runtime/sbx.go:170-174`
  - ADR 0006 の実測「sandbox スコープ rule は sandbox の作成前には置けない」
- **根拠**
  - `network-policy@1` は kit の descriptor で `runtime.allow` を宣言する。runtime は deny-by-default と、sandbox から迂回できない境界での強制を「MUST」で負う（[network-policy@1.md](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/docs/spec/capabilities/com.docker.sandbox/network-policy@1.md)）
  - Docker Docs には「Kit allow rules can't grant access beyond your organization's policy.」とある（[Use kits](https://docs.docker.com/ai/sandboxes/customize/use-kits/)）
- **簡略化できるもの（推測）**: kit の宣言が create の時点で効くなら、次の 2 つが不要になる
  - 作成後に rule を足す手順
  - 「VM は作れたが rule を足せなかった」という復旧の分岐
- **確かめていないこと**
  - kit の allow と sbx の global rule（ADR 0008 が収束させるもの）の合成規則。docs が書いているのは「組織の policy を超えられない」ことだけ
  - v3 の permission gate（SPEC-v3 §7.4。widening は承認で止まる）を `sbx env create --auto-approve` が黙らせるか
  - v3 のローカル kit は create 時に build される（docs に「`sbx` pulls published images and builds local or Git sources when creating the sandbox」とある）。repo ごとに egress が違うので、repo ごとに kit を生成して build する費用がかかる。v2 のローカルディレクトリの kit は build しない
  - 項目 1 の全面移行が前提になる
- **確度**: 推測
- **推奨**: 今は採らない。v3 への移行を決めたら、上の 3 点を実 sbx で確かめてから判断する。ADR 0008 の global rule の収束（`sbxr policy sync`）は、kit の外の仕組みなので影響を受けない

### 5. boot の再生・init・materialize を `lifecycle@1` に寄せる案（簡略化の候補・一部は不可）

- **repo**
  - `internal/assets/kits/sbxr-boot/spec.yaml`（v2 の `setup.startup` で boot.sh を起動ごとに実行する）
  - `internal/sandbox/boot.go:21`（init を create 時に `sbx exec` で 1 回走らせる）
  - `internal/sandbox/materialize.go:160-183`（settings.json への再帰的な merge と git identity）
- **根拠**: `lifecycle@1` は次を持つ（[lifecycle@1.md](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/docs/spec/capabilities/com.docker.sandbox/lifecycle@1.md)）
  - `install`（create 時に 1 回。entrypoint より前）
  - `startup`（毎 boot）
  - `files`（start 時に書く。agent の所有、`overwrite`）
  - v2 の `setup.install/startup/files` は `lifecycle@1` に対応付けられる（[FIELD-MAPPING.md](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/skills/migrate-kit-to-v3/FIELD-MAPPING.md)）
- **影響**
  - boot の再生: v2 の `setup.startup` から v3 の `lifecycle@1.startup` へ書き換えるだけで、機能は減らない（推測）
  - init: `install` hook に移すと、create 時に 1 回走ることは spec が保証する。ただし次の 2 点で、今の sbxr の設計と噛み合わない
    - `install` の間だけ開く network は kit の宣言で決まる（`network-policy@1` の `install` の phase）
    - repo の宣言ごとに kit を生成して build する必要がある
  - materialize
    - `files` はファイル全体を書くので、sbx が置いた settings.json の初期値に merge する今の処理（`materialize.go:161`）の代わりにはならない（確認済み。`files[].content` は本文そのもの）
    - `files` が agent の所有で書かれることは、ADR 0006 の `sbx cp` の uid 問題を回避する用途にはなりうる（推測）
- **確度**: 推測。settings.json の merge ができないことは確認済み
- **推奨**: 採らない。項目 1 の移行を決めたときに、boot の再生だけを `lifecycle@1.startup` へ移す

### 6. 注入先 host が egress で許可されていることを要求する規則（spec と sbxr の一致）

- **repo**: ADR 0003（「配線するのは、要求され、かつ注入先 host が egress で許可されているときに限る」）、`internal/secret/wiring.go`
- **根拠**
  - SPEC-v3 §11 の cross-entry 検証は「every credential inject domain appears in the matching phase of the network policy's allow list」を求める（[SPEC-v3.md §11](https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/docs/spec/SPEC-v3.md#11-validation-summary)）
  - `network-policy@1` の頁は理由として「an inject domain outside the allow list would be a credential mapped onto a connection that can never occur」を挙げる
- **影響**
  - spec の検証は kit の中の宣言にしか効かない。sbxr の宣言（secrets.env と secret 定義）の検証は、sbxr が持ち続ける
  - sbxr が同じ規則を独立に選んでいたことの裏付けにはなる
- **確度**: 確認済み
- **推奨**: 何もしない

### 7. 影響なし

- **状態ディレクトリ（ADR 0006）**: spec は runtime 側の状態の持ち方を範囲外としている（SPEC-v3 §10）。env 定義を `sbx env rm` のために実在させ続ける前提は sbx の env の仕様で、spec とは関係しない
- **env 定義の schema（`schemaVersion: "1"`）**: spec の外（上の「前提」を参照）
- **global rule の収束（ADR 0008）**: `sbx policy` の global rule は kit の外の仕組みである。v0.45.0 には関係する変更が 2 つあるが、どちらも前提を壊さない（[sbx-releases v0.45.0](https://github.com/docker/sbx-releases/releases/tag/v0.45.0)）
  - `sbx policy ls` が rule の作られ方を示すようになった（`--created-via`）
  - `sbx policy allow network` が不正な pattern を保存前に拒むようになった
  - 前者は、将来「手で足した rule を消す」判断に使える情報になりうる（ADR 0008 は、手で足したものも消すと決めている）
- **sbx v0.45.0 の breaking changes**（Kit v3 と同じ release）
  - `sbx mcp catalog` の削除、`sbx mcp rm` と `sbx secret rm` の error 化: sbxr の非 test の Go コードに `mcp`・`secret rm` の呼び出しは無い（grep で確認）。secret の撤去は `sbx env rm --force` に任せている（`internal/runtime/sbx.go:159-162`）
  - 削除系のコマンドが確認を求めるようになった: sbxr は `env rm --force` を渡している。実測（v0.45.1）もこの後の版で行った
  - `sbx ls --json` に `created_at` と応答しない状態の報告が加わった: `sbxStatus` は `stopped` 以外をすべて稼働中と読む（`internal/runtime/sbx.go:144-149`）。応答しない状態も destroy を拒む側に倒れる
- **clone 方式**: v0.45.0 で「Clone-mode sandboxes restore their host Git remotes on every restart」が入った。system.md の実測 2（v0.45.1）はこの後の版なので、前提は変わらない

## 一次情報で裏付けられなかった点

- **CNCF への寄贈・標準化の話**: 記事は「Docker and CNCF Partnership」の blog を挙げている。しかし `www.docker.com` はこの環境の WebFetch の許可リストに無く、読めなかった。spec の repo にも CNCF への言及は無い（grep で 0 件）。記事のみ・未確認
- **発表日 2026-09-24**: 上と同じ理由で、Docker blog を読めなかった。一次情報で確かめられた日付は次の 2 つ
  - spec の repo の作成日: 2026-09-16
  - v3 に対応した sbx v0.45.0 の release: 2026-09-21
- **Docker 公式の日本語訳**: 未確認
- **OCI として sign・scan できること**: 記事の要約にある主張である。一次情報で確かめられたのは、spec の README が「ordinary OCI image」「one digest」と書いていることまで。sign と scan の手順は確かめていない

一次情報で確かめられた点は次のとおり。

- v3 で experimental であること
- Apache-2.0 であること（spec の repo の LICENSE）
- 最終版を 2026 Q4 に予定していること（README）
- sbx が `sbx run … --kit` で v3 を扱うこと（README、Docker Docs）

## 参照

- 記事（二次情報）: https://gihyo.jp/article/2026/09/docker-sandbox-kit-spec
- spec の repo（commit `b1c53cb2690b63fdab272c1d8519704e1375143e`）: https://github.com/docker/sandbox-kit-spec
  - README: https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/README.md
  - SPEC-v3: https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/docs/spec/SPEC-v3.md
  - RELEASES: https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/RELEASES.md
  - lifecycle@1: https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/docs/spec/capabilities/com.docker.sandbox/lifecycle@1.md
  - network-policy@1: https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/docs/spec/capabilities/com.docker.sandbox/network-policy@1.md
  - credential@1: https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/docs/spec/capabilities/com.docker.sandbox/credential@1.md
  - sbxenv.yaml（sbx の env 定義の例）: https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/sbxenv.yaml
  - v2 → v3 の対応表: https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/skills/migrate-kit-to-v3/FIELD-MAPPING.md
  - 移行の手順: https://github.com/docker/sandbox-kit-spec/blob/b1c53cb2690b63fdab272c1d8519704e1375143e/skills/migrate-kit-to-v3/SKILL.md
- v2 の spec（v3 が後継とするもの）: https://github.com/docker/sbx-kits-contrib/blob/main/spec/SPEC-v2.md
- sbx v0.45.0 の release notes: https://github.com/docker/sbx-releases/releases/tag/v0.45.0
- Docker Docs
  - Kits: https://docs.docker.com/ai/sandboxes/customize/
  - Kits v2: https://docs.docker.com/ai/sandboxes/customize/kits-v2/
  - Use kits: https://docs.docker.com/ai/sandboxes/customize/use-kits/
- 読めなかったもの（hook により取得不可）
  - https://www.docker.com/blog/docker-sandbox-kit-spec/
  - https://www.docker.com/blog/docker-sandbox-kit-spec-cncf/
