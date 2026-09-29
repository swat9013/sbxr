# 0012. doctor で宣言と host の前提を全件検査する（plan と分ける）

- Status: Accepted
- Date: 2026-09-29
- 覆す条件: plan が、最初の誤りで止まらずに全項目の結果を返す形へ変わったとき（doctor と plan の検査が同じ出力に畳める）

## 決定

- `sbxr doctor [<repo>]` を足す。host の前提と 3 スコープの宣言を、規則に照らして全件検査する（診断）。`<repo>` は plan と同じく path と git URL を受け、省くと環境と user 側だけを見る
- 検査項目はそれぞれ ok・fail・skip のどれかになる。fail には直し方を 1 行付け、fail が 1 つでもあれば非 0 で終える
- スコープごとに独立して検証し、前段の失敗で後段を飛ばさない。skip にするのは、検査に要る入力が他の項目の失敗で得られないときだけにする
  - user 設定が通らない: herdr・global rule・git identity・secret の配線と値を skip する。repo 宣言のファイル単位の検証（書式・型・未知の key・スコープ制限）と、repo の egress の group の検証は続ける
  - repo 宣言が通らない: merge 後の git identity と secret の配線と値を skip する
  - sbx が無い: global rule を skip する
  - secret ファイルが読めない: secret の値を skip する
- 検査の対象は、create が同じ入力で止まる条件に揃える。`<repo>` を渡したときの secret の値は、merge 後に配線される secret のものを見る（repo 宣言が足した要求も含む）
- host の管理状態・sandbox VM・global rule のどれも変えない。VM の状態（drift など）は見ない
- 出力はテキストだけにする

## 根拠

- plan は作られる内容の要約で、要約を作れない誤りに当たると、そこで止まる。user 設定に誤りがあると repo 宣言を読まず、git identity などの後段の検査は前段が通ってから走る。AI に設定を書かせて「書く → 検査する → 直す」を回すと、誤りが 1 つずつしか見えず、往復が誤りの数だけ増える
- 設定を書かせる skill には schema の全体を書かない（版ずれを避ける）。doctor の error が、どの key が・なぜ駄目で・どう直すかを返し、skill の知識の不足を埋める
- repo の egress は sandbox スコープ rule として default と user とは別に重ねるので、group が揃っているかは user 設定に依らずに決まる。user 設定の誤りで skip する理由が無い

## 却下した代替案

- plan に「全件検査」のモードを足す: plan の役目（作られる内容と drift を見せる）と、宣言の誤りを並べる役目が 1 つの出力に混ざる。drift を出すために VM の状態を読むことも、診断には要らない
- 最初の fail で止める: 上の往復の問題が残る
- `--json` を最初から出す: 読み手（skill と人）はテキストで足りる。要望が出てから足す
