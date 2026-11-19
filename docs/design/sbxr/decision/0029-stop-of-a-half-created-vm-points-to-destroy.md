# 0029. 作成途中の VM を止めた `sbxr stop` は、`sbxr start` でなく `sbxr destroy` を案内する

- Status: Accepted
- Date: 2026-11-19

## 決定

- 作成途中で稼働中の VM を止めた `sbxr stop` は、止めた後に `sbxr destroy <repo>` で片付けてから `sbxr create <repo>` で作る手順を示す。`sbxr start <repo>` は示さない
- この案内は herdr の状態に依らずに出す。herdr 連携を有効にせずに作った VM と、herdr machine の登録が無い VM（UC3 の 3a・3c が `sbxr start` の案内を行わないとする経路）でも出す
- herdr machine を無効にしたとき（同じ名前の前の VM の登録が残っていたとき）は、無効にしたことだけを示し、有効に戻す手順（`sbxr start`）は添えない
- 作成済みの VM の案内は `sbxr start <repo>` のまま変えない（decision/0025）

## 根拠

- `sbxr start` は作成途中の VM を拒んで destroy を促す（decision/0025、UC10 の 2b）。stop が start を案内すると、案内どおりに打ったコマンドが拒まれる
- 作成途中の VM に sbxr が勧める操作は destroy だけである（decision/0025）。勧める操作は VM が作成途中かで決まり、herdr の状態では変わらない。そのため、herdr の状態で案内を出し分ける作成済みの VM の経路（3a・3c）と分ける

## 却下した代替案

- 作成途中の VM の stop では何も案内しない: 食い違いは消えるが、止めた後に打つコマンドが利用者に見えない。作成途中の VM は destroy しないと create も start も通らない
- start が作成途中の VM を受け付ける: start の振る舞いを変える。作成途中の VM は作成の後段（VM の中の確認・作成時の宣言の記録）を経ておらず、起こしても使える保証が無い

## 見直し条件

置かない（案内の文言の決定で、実行の記録から測れる量が無い）
