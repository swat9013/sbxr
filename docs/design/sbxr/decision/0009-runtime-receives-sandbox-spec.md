# 0009. Runtime port は作る内容を domain の言葉で受け、kit・startup log・順序の制約を adapter に持たせる

- Status: Accepted
- Date: 2026-09-27
- 覆す条件: 2 つ目の本番の実行基盤を足すとき、この形では作る内容を表せないと分かったとき

## 決定

- Runtime は sandbox VM の「作る内容」を domain の言葉で受け取る。中身は、名前・repo・VM の環境変数・herdr の導入と版・sandbox スコープの secret（値を含む）・sandbox スコープ rule の宛先
- 作成は 2 つの手順に分ける
  - **定義**: 状態ディレクトリに、実行基盤が VM を作るための定義を書く。sbx では env 定義と kit
  - **作成**: 定義から VM を作る。sandbox スコープの secret を置いてから VM を作り、作った後に sandbox スコープ rule を足し、herdr を導入するなら VM の起動時の処理が終わるのを待つ
- 次のものは Sbx adapter の内側に置く
  - env 定義の schema
  - kit の置き場と参照
  - kit dispatcher の startup log の文面と完了の待ち
  - secret と rule の順序
- kit の資材は `internal/assets` に残し、Sbx adapter から使う
- 起動ごとに boot を再生する処理は、作る内容の項目にしない。adapter は常に再生の仕組みを入れ、再生する script の置き場（VM の home からの path）は Runtime が公開する。boot の宣言が空なら script を置かないので、何も走らない
- VM を作れた後の段（rule の追加・起動時の処理の待ち）で失敗したら、adapter は「VM は作れた」と分かる error を返す。lifecycle は、これを見て復旧手順（destroy して作り直す）を選ぶ
- 作成の最初に書いた定義から herdr 連携の有無を読む問いを、Runtime に置く。作成途中の VM の stop・destroy が herdr machine を扱うかを決めるのに使う
- secret の配線（`internal/secret`）は Runtime に依存しない。置く secret の形（値を含む）を出すまでが配線の担当

## 根拠

- env 定義の schema・kit の `./` 相対参照・startup log の文面は、どれも sbx v0.45.1 に合わせた約束で、実行基盤を替えれば一緒に変わる（system.md の port の切り方）。domain に置くと、実行基盤の差し替えが domain の書き換えになる
- 順序の制約（secret は作成前、rule は作成後）は sbx の実測（ADR 0006）で、別の実行基盤では違いうる。domain が呼ぶ順で守ると、実行基盤ごとの順序が domain に漏れる
- 定義と作成を分けるのは、状態ディレクトリへ書く順序を「定義 → 出所」に保つため。出所を先に書いて定義で失敗すると、destroy が定義の無い状態ディレクトリで詰む（ADR 0006 の実測）。1 回の呼び出しにまとめると、出所を定義より先に書くしかなくなる
- boot の再生を項目にしないのは、今の sbxr が boot の宣言の有無にかかわらず再生の仕組みを入れており、項目にしても常に同じ値になるから
- herdr 連携の有無の問いを Runtime に置くのは、定義の中身を読めるのが adapter だけになるから
- herdr 連携が Runtime の interface に現れることは受け入れた（kit を domain に残す案は、kit の参照と env 定義の schema が domain に残る）

## 却下した代替案

- 作る内容を 1 回の呼び出しで受け、定義も作成も adapter が行う: 状態ディレクトリへ書く順序（定義 → 出所）を守れない
- kit を domain に残し、env 定義の組み立てだけを adapter に移す: kit の参照（`./kits/<name>`）と startup log の文面が domain に残り、実行基盤の約束が 2 か所に分かれる
- 作る内容を確認関門の `Prepared` に載せる: secret の値が確認関門と plan の表示の経路に乗る
