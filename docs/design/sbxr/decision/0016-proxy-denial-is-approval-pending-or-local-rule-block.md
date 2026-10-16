# 0016. proxy の拒否応答を、sbx v0.48.0 の承認待ちと deny rule の拒否に置き換える

- Status: Accepted
- Date: 2026-10-16
- 覆す条件: sbx の proxy が許可外の宛先へ返す応答（状態コード 403 と、下の 2 つの body）が変わったとき。または、承認待ちの通信が承認を待たずに宛先へ届くようになったとき。実測は sbx v0.48.0（#91）

## 決定

[decision/0007](0007-egress-self-check-verdict-by-proxy-denial.md) の「覆す条件」（proxy の拒否応答が変わったとき）が成り立った。0007 の判定の表・許可先と許可外の選び方・probe 先が無いときの扱いは保ち、**proxy の拒否応答** の定義だけを次に置き換える。

- 状態コードが 403 で、body が次のどちらかで始まる応答。`<host>` は probe した宛先
  - `Approval required for <host>:443.`: どの rule にも当たらない宛先（default deny）。sbx はこの通信を承認待ちとして記録する
  - `Blocked by local rule for <host>:443`: deny rule に当たった宛先
- 承認待ちは「届かない」とみなす
- v0.45.1 の body（`Blocked by network policy`）は受けない

## 根拠

- sbx v0.48.0 の proxy は、どの rule にも当たらない宛先へ 403 と次の body を返す（2026-10-16 の実測）

  ```
  Approval required for example.com:443.

  Review and respond with:
    sbx policy approval ls
  ```

  `sbx policy approval ls` には、その sandbox の承認待ちが 1 件（`no matching allow rule (default deny)`、選択肢は allow と dismiss）現れる
- 承認待ちの間、同じ宛先へ送り直しても 403 が返る（実測）。承認待ちの通信は宛先へ届いていない。承認は利用者が `sbx policy approval respond` で明示に行う操作で、承認するまで default deny のまま効いている。egress 自己検証が確かめたいのは「許可外へは proxy の policy が通さない」ことなので、承認待ちは「届かない」に当たる
- deny rule に当たった宛先へは、403 と body `Blocked by local rule for example.net:443` が返る（sandbox スコープの deny rule で実測）。許可外の候補を利用者が deny rule で塞いでいても、egress 自己検証が「届いた」と誤判定しないよう、こちらも拒否応答に含める
- body の先頭を宛先の host と port まで含めて照合するのは、0007 と同じく、TLS を終端する proxy の応答と宛先自身の 403 を区別するため。文言だけで照合するより、宛先自身の 403 の body と取り違える余地が狭い
- v0.45.1 の body を受けないのは、#91 の方針（sbx の両方の版を通す分岐を作らない）に従うため。sbxr が対応する sbx の版の下限は #99 で決める

## 影響

- create のたびに、許可外の候補（`example.com` など）への承認待ちが sbx に 1 件残る。sandbox を撤去すると消える（実測）。利用者が allow すると、その sandbox だけがその宛先へ届くようになる

## 却下した代替案

- 承認待ちを「届いた」とみなす: default deny が効いていて宛先へは届いていないのに、v0.48.0 の上では create が必ず egress 自己検証で止まる
- 状態コード 403 だけで判定する: 0007 と同じく、宛先自身の 403 と proxy の拒否を区別できない
- body の文言だけ（宛先の host を含めずに `Approval required for`・`Blocked by`）で照合する: 宛先自身の 403 の body が同じ語で始まると、許可先の probe が「届かない」と誤判定する。照合の幅を広げる理由が無い
- v0.45.1 の `Blocked by network policy` も受ける: 両方の版を通す分岐になる（#91 の方針に反する）
- probe の後に承認待ちを `sbx policy approval respond` で dismiss する: sbx の別のコマンドへの依存が増え、dismiss に失敗したときの扱いを新たに決める必要がある。残った承認待ちは sandbox と一緒に消え、利用者が allow しない限り通信は通らない
