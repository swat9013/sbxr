# 0003. destroy は作成済みの clone・copy の VM の未回収を一時起動して検査し、--force で省く

- Status: Accepted
- Date: 2026-09-26
- 覆す条件: [system.md の未実測の前提](../system.md#未実測の前提) 2 が成り立たないとき

## 決定

- **検査する対象**: 作成済み（稼働中・停止中）で、投入方式が clone か copy の VM。destroy は確認の前にこの検査を行う
- **未回収の定義**: 次の 3 つのどれか
  - origin の remote-tracking（VM 内で fetch した後のもの）から到達できない local branch の commit
  - 未 commit の変更
  - stash
- **停止中の VM**: 一時起動して検査し、止め直す
- **未回収があったとき**: 撤去せずに止まる
- **`--force`**: 稼働中の VM の撤去に加えて、この検査も省く
- **検査しないもの**: mount の VM、作成途中の VM、VM 消失

## 根拠

- clone・copy の VM の作業は、origin に出したもの以外は destroy で失われる
- 検査には VM の稼働が要るが、destroy は稼働中の VM を `--force` 無しでは拒む。一時起動して元の停止に戻すのが、両方を満たす形になる
- 作成途中の VM は agent に渡しておらず、repo の投入が済んでいないこともある。そのため検査自体が失敗しうる
- `--force` は、#9 の記述どおり 1 つの flag にまとめた（利用者の選択）

## 残るリスク

- 検査は VM 内で origin を fetch する。origin の host の egress 許可か secret の配線が無い repo では検査が毎回失敗し、`--force` しか道が無い
- 一時起動した VM を止め直す `sbx stop` は、検査の間に attach した session も切る（ADR 0006）

## ADR との関係

ADR 0006 は「`--force` を使用中の VM の強制撤去だけに使う」と約束している。この決定はその約束を広げるので、ADR 0006 への追記か新しい ADR が要る。リポジトリ全体の ADR へ昇格する候補。

## 却下した代替案

- 稼働中の VM でだけ検査する: 停止中の VM で未回収を取りこぼす
- stop のときに検査して状態ディレクトリへ記録し、destroy はそれを読む: sbxr の外で起動・停止されると、記録が古くなる
- 別の flag（例 `--discard-unrecovered`）にする: ADR 0006 の約束は保てるが、利用者は #9 どおり `--force` にまとめることを選んだ
- 作成途中の VM も検査する: 検査自体が失敗しうるので、`--force` が常に要る
