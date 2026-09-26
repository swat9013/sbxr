# 0005. build 用 VM には inputs のファイルだけを置き、template は期限で作り直す

- Status: Accepted
- Date: 2026-09-26
- 覆す条件: [system.md の未実測の前提](../system.md#未実測の前提) 1 が成り立たないとき

## 決定

- **build 用 VM に置くもの**: 宣言の `inputs` のファイルだけを、repo と同じ相対 path に置いて `run` を走らせる
- **inputs の制約**: repo root からの相対 path に限る。`..`・絶対 path・repo の外を指す symlink は書けない
- **build 用 VM の配線**: egress と secret の配線は、本番の sandbox VM と同じにする
- **build 用 VM の識別と撤去**: 名前は `sbxr-build-<出所の hash>` とし、状態ディレクトリに build の印と出所を持たせる。template の作成が終われば、成否によらず撤去する。残骸は同じ出所の次の create が撤去する。撤去するのは印と出所が一致するものだけ（ADR 0006 の、状態ディレクトリの無い VM には触らないという約束を保つ）
- **期限**: template は作成日時から期限（default 7 日。user 設定の `template.max_age_days` で変える。repo 宣言は書けない）を過ぎたら、create が作り直す。作り直した image には同じ tag を付け替える

## 根拠

- inputs だけを置けば、hash と template の中身が 1 対 1 に対応する。inputs への書き忘れは、build の失敗として表に出る
- template は基盤の image（VM 内の Claude Code を含む）を、作った時点で固定する。期限を設けるのは、それを古いまま使い続けないため
- secret は placeholder 注入なので、snapshot に実値は入らない
- repo 宣言は untrusted なので、inputs を通して host のファイルを読ませない

## 却下した代替案

- repo 全体を置き、snapshot の前に消す: hash に入っていないファイルが template の中身に効く
- source 無しで走らせ、tool の版を宣言に直接書く: repo の lockfile と版を二重に管理することになる
- 基盤の更新を取り込まない（利用者が `sbx template rm` で消す）: VM 内の Claude Code が古いまま残る
- sbxr の版を hash に含める: 基盤の image の更新と sbxr の更新は連動しないので、効果が間接的
