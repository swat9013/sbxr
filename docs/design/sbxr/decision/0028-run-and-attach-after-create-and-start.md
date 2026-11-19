# 0028. create と start の終わりに、宣言の run を裏で始め、`--attach` で agent の対話 session に入る

- Status: Accepted
- Date: 2026-11-19

## 決定

- 宣言に `run`（コマンドの list）を足す。create と start の終わりに、VM 内の作業ツリーで裏で始める
  - 3 スコープで書け、`init`・`boot` と同じく並べて足す。確認プロンプトと plan に内容を示す
  - 作成時の宣言に記録し、drift で比べる。start は記録した内容を始め、宣言を読み直さない（boot と同じ）
  - sbxr は始めたことと、出力が残る VM 内の log の場所を示して終わる。終わるのを待たない
  - 始めるのは、VM の中の確認（decision/0027）と、herdr machine の登録・有効化の後にする
  - 始められなければ、VM は残し、非 0 で終える
  - start は、VM が既に稼働していたら始めない（動いている run を二重に始めない）
  - sbxr の外での起動（外部起動）では始めない
- `sbxr create` と `sbxr start` に `--attach` を足す。終わりに、VM 内の作業ツリーで agent の対話 session に入る。抜けても VM は動いたままにする
  - 端末が無ければ、VM に触れる前に止まる
  - run があれば、run を始めた後に入る
  - `sbxr recreate` は create と同じ flag を受ける
- create のたびに変わる指示を引数で渡す flag（`--prompt` など）は足さない。指示は、宣言の run に agent の非対話の実行（`claude -p` など）として書く

## 根拠

- 目的の「手軽」（decision/0023）に対し、立ち上げた後に作業を始めるまでが sbxr の外に残っていた。利用者は create の後に `sbx exec` か herdr で VM に入り、自分で agent を起動していた
- init と boot は準備で終わる。boot は VM の起動時の処理として走るので、端末に繋がらず、VM の起動を待たせる。長く走る作業を置く場所ではない
- run は VM の中でだけ走るので、untrusted な repo 宣言に書かせても隔離は弱まらない（init・boot と同じ扱い）

## 却下した代替案

- boot に書かせる: boot は外部起動でも走り、終わるまで VM の起動時の処理が終わらない。長く走る agent の実行を置くと、create と start が完了を待ち続ける
- `--prompt` で指示を渡す: 同じ VM へ 2 回目の指示を出す経路（start に付けるか、稼働中の VM へ送るか）まで決める必要がある。宣言の run で同じことができる
- attach を既定にする: `--yes` での無人の実行と、herdr の pane から入る使い方では、端末を繋ぐ必要が無い

## 見直し条件

置かない（宣言の key と flag の有無の決定で、実行の記録から測れる量が無い）
