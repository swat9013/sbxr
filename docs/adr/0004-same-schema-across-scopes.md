# 3 スコープで同じ schema を使い、全 VM 共通の egress は user 設定に置く

Status: accepted (2026-09-25)

default（CLI に同梱）・user（`~/.config/sbxr/config.yaml`）・repo（`sbxr.yaml`）の 3 スコープで同じ key 体系（`version` / `profile` / `git` / `egress` / `init` / `boot` / `secret_defs` / `secrets` / `herdr`）を使う。同じ key はどのスコープでも同じ型を持つ。スコープごとの差は 1 つの制限表で定義する（例: repo の egress は sandbox スコープ rule になる、repo は base が固定した key を上書きできない、repo は `secret_defs` を書けない — ADR 0003、repo は `herdr` を書けない — ADR 0007、repo は egress group の `enabled` を書けない — ADR 0008）。先頭の `version: 1` で将来の schema 変更を CLI が検出できるようにする。

egress は sbxr 独自の形式で書き、実行基盤の rule へは adapter が変換する。宛先の置き場は「全 VM 共通 = user 設定（global rule）」「repo 固有 = repo 宣言（sandbox スコープ rule）」の 2 つで、汎用の宛先グループは default に同梱する。group の `allow` の書式、除外、global rule の収束は ADR 0008 が決める。

旧実装は「user スコープに egress を書けない、全 VM 共通の宛先は別ファイル（`network.json`、sbx の rule 形式そのまま）」としていた。この規則の目的は置き場を 3 つに増やさないことで、1 ファイルにまとめても置き場は 2 つのまま保たれる。

## 改訂（2026-09-26、#31）

設計ドキュメント（[docs/design/sbxr/](../design/sbxr/)）の決定で、key 体系と制限表を次のように改める。

- key 体系に `template`（`run`・`inputs`・`max_age_days`）を足す。repo の source を渡す前に走らせ、template に焼く cache 層を書く（[decision/0004](../design/sbxr/decision/0004-template-per-repo-built-in-create.md)・[decision/0005](../design/sbxr/decision/0005-template-build-inputs-and-refresh.md)）
- 制限表: repo は `template.run` と `template.inputs` を書けるが、`template.max_age_days` は書けない
- 重ね方: `run` は init と同じく並べて足し、`inputs` は secrets と同じく和集合、`max_age_days` は override
- repo の投入方式（clone・copy・mount）は宣言の key にせず、create の flag でだけ選ぶ（[decision/0002](../design/sbxr/decision/0002-workspace-modes.md)）

## 改訂（2026-09-28、#47）

スコープ制限の表を、すべての深さで fail-closed にする。

- repo 宣言に書かれた key は、すべての深さまで列挙して表で引く。これまでは profile・git・herdr・egress の group の 1 段下までしか列挙しておらず、表で親だけを許した key の子を repo が書けた（上の改訂の `template.max_age_days` を禁じるには、`template` の下まで引く必要がある）
- 表の各行は、「この key だけ書ける」か「この下は全部書ける」（例: `profile.enabledPlugins`・`egress.*.allow`）かを明示する。書けない key も表に行を持たせる
- 宣言の型が持つすべての key が、表の中で「書ける」「書けない」のどちらかに分類されていることを、型から key を数える test で確かめる。型に key を足して表に分類を足し忘れると、test が止める
- 表に無い key の既定は、これまでどおり「repo は書けない」

宣言の検証を 2 段にし、誤りの出所を error に付ける（#53 で追記）。

- 書式・型・未知の field は、スコープごと・ファイルごとに検証する。egress の group と secret 定義も、config が egress・secret の型で読み、その場で検証する（依存は config → egress・secret の向き。egress と secret は config に依存しない）。error には、そのファイルとスコープを付ける
- egress の group の中身が揃っているか（ADR 0008 の「除外した group にも rationale と allow を求める」）は、merge の後に検証する。上の層が `enabled: false` だけを重ねる書き方があるので、1 ファイルでは決まらない。error には、その group 名を最初に書いたスコープを付ける
- merge の規則は 1 か所に置く。global rule と secret 定義だけを使う操作（`sbxr policy sync`・`sbxr secret setup`）も、repo を空の宣言として同じ merge を通す。git identity の要求は merge ではなく、それを使う側が確かめる
- config は、検証済みの型（global rule の宛先・sandbox スコープ rule の宛先・secret 定義）を返す。利用者は egress と secret の検証を組み直さない
- `create --yes` で落とす repo の egress も検証し、不正なら止める。落とすかどうかで、plan と create の error を変えない
