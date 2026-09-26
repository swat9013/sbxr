# egress の allow は sbx の host pattern に絞り、global rule は宣言へ全面的に収束させる

Status: accepted (2026-09-27)

egress 宣言（ADR 0004）の group は `rationale` と `allow` と `enabled` を持つ。`sbxr policy sync` は、default と user 設定の有効な group の `allow` を重複なく並べた集合を期待集合とし、sbx の global rule をそこへ収束させる。

## allow の書式

`allow` の 1 entry は、sbx の host pattern（`*` / `**` / `?` / `[]` の glob）に任意の `:port` を付けたものだけを受け付ける。port は 1〜65535。次は宣言の読み込みで error にする。

- 末尾 2 label に glob を含むもの（`**.com`・`**.*` など）。全開に近い指定を宣言に書かせない
- 大文字を含む host。収束は宣言と sbx が保存した rule を文字列で突き合わせるので、宣言の側を sbx が保存する形に揃える
- IPv6 と CIDR。IPv4 は host と同じ書式として通る
- カンマ。sbx は 1 つの引数の中のカンマを宛先の区切りとして読むので、1 entry が複数の宛先になる

## group の除外

group の除外は、既存の group に `enabled: false` を重ねて書く。除外した group にも `rationale` と `allow` を求める。中身の無い group は、除外したかった group 名の書き違いで生まれた新しい group なので、黙って通さずに止める。

repo 宣言は `enabled` を書けない（スコープ制限表、ADR 0004）。repo の egress は sandbox スコープ rule になり、global rule を除外できないため。

## global rule の収束

- 対象は `sbx policy ls --json` の rule のうち、`scope=global`・`resource_type=network`・`editable=true` のもの。sbx 自身が管理する rule と sandbox スコープ rule には触れない
- 残す rule は「allow で 1 resource、期待集合にあり、同じ宛先を他の rule がまだ担っていない」ものに限る。それ以外（期待集合に無い・deny・複数 resource・重複）は消す。宣言に無い rule は、sbxr を経由せず手で足したものも消す。何が消えるかは `sbxr policy sync --check` の差分表示で事前に見える
- 宛先を足してから消す。複数 resource の rule を 1 resource の rule に分けるとき、宣言に残る宛先が一時的にも塞がらないようにするため
- 途中で書き込みに失敗したら、そこまでに行った変更を error と一緒に返す。適用後に読み直し、期待集合と一致しなければ error にする

## 未確認

sbx が host の大小文字や port の無い pattern（例: `*.example.com`）を正規化して保存するかは確かめていない。確かめるには実 sbx の global rule へ書き込む必要があり、global rule は稼働中の全 sandbox VM に即座に効くため、#3 では読み取り（`sbx policy ls --json` と `sbxr policy sync --check`）だけを行った。sbx が保存時に書き換えるなら、その宛先は毎回の sync で消して足し直すことになる。そのときは宣言の書式を保存形に合わせて絞るか、突き合わせの前に正規化する。

## Considered Options

- allow の書式を検査せず sbx に渡す: sbx が受け付ける全開に近い指定がそのまま global rule になり、全 sandbox VM に効く。宣言と保存形の食い違いも、毎回の sync での消して足し直しとしてしか現れない
- 大文字の host を受け付け、sbxr が小文字に直してから渡す: sbx の保存形を確かめていない段階で、sbxr の側に正規化の規則を持つことになる。拒否なら利用者が書き直すだけで済む
- 除外を `enabled: false` だけで書ける形にする: 書き違えた group 名が中身の無い新しい group として通り、除外したつもりの group が有効なまま残る
- 除外を group の外の list（例: `egress_disabled: [github]`）で書く: #2 の scalar override に乗らず、merge の規則が 1 つ増える
- 宛先ごとの型（`Resource`）を作る: 期待集合を作る経路は検証済みの group からの 1 本だけで、型で守るものが無い
- sbxr が足した rule だけを消す、または削除を `--prune` のような opt-in にする: global rule の正本が宣言と sbx の rule 表の 2 つに分かれる。sbxr が足したかを見分けるには、その記録を別に持つ必要もある
- 消してから足す: 手順は単純になるが、複数 resource の rule を分ける間、宣言に残る宛先が塞がる
