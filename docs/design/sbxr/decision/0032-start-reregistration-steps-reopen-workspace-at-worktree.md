# 0032. start が示す登録し直す手順は、最初の workspace を VM 内の作業ツリーで開き直す段まで含める

- Status: Accepted
- Date: 2026-11-19

## 決定

- `sbxr start` が herdr machine の登録が無いとき（UC10 の 5b。利用者が手で解除した等）に示す、登録し直す手順の末尾に、最初の workspace を VM 内の作業ツリーで開き直す段を足す
  - 開き直す段の中身は [decision/0015](0015-herdr-first-workspace-reopened-after-registration.md) と同じ。作業ツリーを `--cwd` にした workspace を開き、開く前からあった workspace を閉じる
- 手順は利用者が手で実行する。sbxr は開き直す段を実行しない。5b の振る舞い（手順を示して非 0 で終える）は変えない
- 手順の順序は次のとおり
  1. VM 内の herdr server を止める
  2. `herdr machine add` で登録する
  3. VM の中で `herdr workspace list` を実行し、今ある workspace の id を控える
  4. `herdr workspace create --cwd <作業ツリー> --focus` を実行する
  5. 4 が通ったときだけ、3 で控えた id の workspace を `herdr workspace close <id>` で閉じる。4 と 5 は `&&` でつないだ 1 つのコマンドとして示す（decision/0015 の「開けたときだけ閉じる形」）
- 閉じる対象を、create の前に控えた id に限る。create の後の一覧では、開いた workspace と前の workspace を見分けられないため。開けなかったときは何も閉じないので、workspace が 0 件になる間を作らない（decision/0015 と同じ）
- 作業ツリーの path は、start が `<repo>` から求めた VM 内の作業ツリーを使う。run を始める場所（decision/0028）と同じ値
- decision/0015 が create の経路に定めた自動の開き直しは変えない

## 根拠

- 手順どおりに登録し直すと、ssh 経由で起動し直した herdr server は最初の workspace を `/home/agent/workspace` で作る（decision/0015 の実測）。登録だけを示すと、pane が作業ツリーで始まらず、decision/0014「herdr の pane を VM 内の作業ツリーから始める」に反する
- 開き直しの手段は decision/0015 の実測で確かめてある（`herdr workspace create --cwd <作業ツリー> --focus` の後に、前の workspace を閉じる）。手で示す手順も同じ手段にそろえる

## 却下した代替案

- 5b で sbxr が登録し直してから自動で開き直す: 5b の振る舞い（手順を示して非 0 で終える）を変えることになる。登録が無い理由（利用者が意図して解除した等）を sbxr は知らないので、登録を勝手に作り直さない
- 登録だけを示し、開き直しは kit の `new_cwd` に任せる: `new_cwd` は最初の workspace に効かない（decision/0015 の実測）
- 「list に出る他の workspace を閉じる」と示す: create の後の一覧には開いた workspace も載るので、利用者がそれを閉じうる。全部閉じれば 0 件になる

## 見直し条件

decision/0015 の覆す条件と同じ（`herdr machine add` が VM 内の server の cwd か最初の workspace の cwd を指定する手段を持ったとき）。そのときは、示す手順から開き直す段を外す
