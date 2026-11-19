# 0030. 設定の書き方の案内を binary に埋め込み、`sbxr guide` で出す

- Status: Accepted
- Date: 2026-11-19

## 決定

- repo 宣言と user 設定を host の agent に書かせる案内を、sbxr の binary に埋め込み、`sbxr guide` で標準出力へ出す。引数と flag は取らない（decision/0013 の、skill として repo に置き `gh skill install` で入れる部分を取り下げる）
- 案内を設定の書き方の正本にする。skill `sbxr-config` は消す
- 案内が持つもの
  - 書き込みの規則（既存のファイルは差分を見せて承認を得てから書く。secret の値・`sbxr policy sync`・user 設定の egress・sbx と herdr の導入・`sbxr create` は人が行う）
  - 手順（user 設定を先に作る。repo 宣言は repo の中身から推測する。`sbxr doctor` が通るまで直し、止まる条件で止まる。`sbxr plan` で人に確かめてもらう）
  - user 設定で人に聞く項目と、repo 宣言の推測の手がかり
  - 両スコープの key の全一覧（1 行 1 key。意味と、repo 宣言に書けるか）
  - スコープごとの yaml の例
- 案内は、利用者の手元に無いもの（repo の文書・設計 doc・ADR・issue 番号・`internal/`）を指さない
- 案内と code のずれは test で止める
  - 宣言の型が読む key と、案内の key の表の行が一致する
  - 表の「repo 宣言に書けるか」が、スコープ制限表の判定と一致する
  - yaml の例が、そのスコープの検証を通る
- decision/0013 のうち、次は変えない
  - sbxr は設定を生成せず、agent を呼ばない
  - agent が頼る sbxr の出力の形（doctor の項目ごとの状態・fail の直し方・終了コード、plan の `global_egress`）。案内の手順がこれに頼るので、sbxr の側でこれを変えるときは案内も直す
- 互換の対象は subcommand `guide` だけで、案内の文面は対象にしない（CONTRIBUTING.md「v1.0.0 からの互換」の出力の文面）

## 根拠

- 目的の「手軽」（decision/0023）に対し、skill は sbxr の binary とは別に入れる必要があった。binary だけでは、agent が設定の書き方を知る手段が無かった
- skill は binary と別の版で上がるので、skill の書き方が、入れた binary の schema とずれうる。埋め込んだ案内は、入れた binary と同じ版の schema を必ず出す
- 版がずれないので、decision/0013 が skill に置かなかった key の全一覧を案内に置ける。key の一覧は test で code と照合できる
- skill の利用者はまだいないので、互換のために skill を残す相手がいない

## 却下した代替案

- skill を、`sbxr guide` を指すだけの入口として残す: 頼むだけで起動できる入口は残るが、守る利用者がいない。配る物が 2 つになり、README の手順も 2 通りになる
- skill の全文を残し、案内にも同じ内容を書く: key の網羅は test で検査できても、規則の文面の一致は検査できず、いずれずれる
- 節を選んで出す flag（`--section` など）を足す: 求めは binary だけで案内を読めることで、全文を出せば足りる。agent は file に保存して要る節を引ける
- JSON Schema を出す: スコープ制限と merge 後の検証（egress group が揃っているか）は伝わらない（decision/0013 と同じ理由）。key の意味と repo 宣言に書けるかは、案内の表で伝える

## 見直し条件

置かない（配り方と正本の置き場の決定で、実行の記録から測れる量が無い）。効いたかは、CONTRIBUTING.md「v1.0.0 の条件」の手軽の判定条件（skill を入れていない agent が、`sbxr guide` を読んで doctor の通る設定を書く）を実 sbx で確かめるときに見る
