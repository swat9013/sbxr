# egress の allow は sbx の host pattern に絞り、global rule は宣言へ全面的に収束させる

Status: accepted (2026-09-27)

egress 宣言（ADR 0004）の group は `rationale` と `allow` と `enabled` を持つ。group の構造は sbxr 独自の形式で、`allow` の 1 entry には sbx の host pattern をそのまま書く。`sbxr policy sync` は、default と user 設定の有効な group の `allow` を重複なく並べた集合を期待集合とし、sbx の global rule をそこへ収束させる。

## allow の書式

`allow` の 1 entry は、sbx の host pattern（`*` / `**` / `?` / `[]` の glob）に任意の `:port` を付けたものだけを受け付ける。port は 1〜65535。次は宣言の読み込みで error にする。

- 末尾 2 label に glob を含むもの（`**.com`・`**.*` など）。全開に近い指定を宣言に書かせない
- 大文字を含む host。収束は宣言と sbx が保存した rule を文字列で突き合わせるので、宣言の側を sbx が保存する形に揃える
- IPv6 と CIDR。IPv4 は host と同じ書式として通る
- カンマ。sbx は 1 つの引数の中のカンマを宛先の区切りとして読むので、1 entry が複数の宛先になる

## group の除外

group の除外は、既存の group に scalar の `enabled: false` を重ねて書く。スコープ間の merge で scalar は後のスコープが上書きする規則にそのまま乗るので、merge の規則が増えない。

除外した group にも `rationale` と `allow` を求める。中身の無い group は、除外したかった group 名の書き違いで生まれた新しい group なので、黙って通さずに止める。

repo 宣言は `enabled` を書けない（ADR 0004 のスコープ制限表の 1 行）。repo の egress は sandbox スコープ rule になり、global rule を除外できないため。

## global rule の収束

- 対象は `sbx policy ls --json` の rule のうち、`scope=global`・`resource_type=network`・`editable=true` のもの。sbx 自身が管理する rule と sandbox スコープ rule には触れない
- 残す rule は「allow で 1 resource、期待集合にあり、同じ宛先を他の rule がまだ担っていない」ものに限る。それ以外（期待集合に無い・deny・複数 resource・重複）は消す。宣言に無い rule は、sbxr を経由せず手で足したものも消す。何が消えるかは `sbxr policy sync --check` の差分表示で事前に見える
- 宛先を足してから消す。複数 resource の rule を 1 resource の rule に分けるとき、宣言に残る宛先が一時的にも塞がらないようにするため

## 未確認

sbx が host の大小文字や port の無い pattern を正規化して保存するかは確かめていない（[system.md](../design/sbxr/system.md) の未実測の前提 7）。確かめるには実 sbx の global rule へ書き込む必要があり、その書き込みは稼働中の全 sandbox VM に即座に効く。確かめる手順は PR #20 の「実機での確認」にある。

## Considered Options

退けた代替案は、実装時の記録（PR #20 の「設計判断」と欠陥探し、#3 の triage）にあるものに限る。

- allow の書式
  - 末尾 2 label が glob だけの宛先（`**.*:443` など）を通す: 全開に近い指定が global rule になり、全 sandbox VM に効く
  - 大文字の host を受け付ける: sbx が保存する形と文字列で一致せず、毎回の sync で消して足し直す
  - 範囲外の port を受け付ける: 欠陥探しで退けた（理由の記録なし）
  - 宛先ごとの型（`Resource`）を作る: 期待集合を作る経路は検証済みの group からの 1 本だけで、型で守るものが無い
  - IPv6 と CIDR、カンマ: 記録なし
- group の除外
  - 除外した group には中身を求めない: 除外したい group 名の書き違いが、中身の無い新しい group として黙って通る
  - 除外を scalar の `enabled: false` 以外で書く: 記録なし（採った理由は上の「group の除外」）
  - repo 宣言に `enabled` を書かせる: 記録なし
- global rule の収束
  - 削除を `--prune` のような opt-in にする: 宣言に無い rule は手で足したものも消すと先に決めた（#3 の triage）ので、opt-in にする理由が無い
  - 消してから足す: 複数 resource の rule を分ける間、宣言に残る宛先が塞がる
