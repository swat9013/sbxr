# 0024. 投入方式から mount を外し、clone と copy の 2 つにする

- Status: Accepted
- Date: 2026-11-19

## 決定

- 投入方式は clone と copy の 2 つにする。`sbxr create --workspace clone|copy` で選び、既定は clone（decision/0002 の mount の部分を取り下げる）
- host の作業ツリーを VM へ直接見せる構成を、sbxr は作らない

## 根拠

- mount では、VM の中から host の作業ツリー（`.git/hooks`・build の script を含む）を書き換えられる。host で次に git や build を実行すると、VM の agent が書いたコードが host で走る
- これは、目的の「agent は sandbox VM の外へ出られない」（decision/0023）を sbxr 自身が弱める構成である。確認プロンプトでリスクを示しても、境界は弱まったまま残る

## 却下した代替案

- 確認プロンプトで 2 つのリスクを示したときだけ許す（decision/0002）: 隔離を最上位に置く優先順と合わない
- 読み取り専用で mount する: agent が作業ツリーに書けず、VM の中で開発できない

## 見直し条件

置かない（境界の置き方の決定で、実行の記録から測れる量が無い）
