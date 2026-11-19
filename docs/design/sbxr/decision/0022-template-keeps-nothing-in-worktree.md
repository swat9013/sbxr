# 0022. template は VM 内の作業ツリーの path に何も残さない

- Status: Accepted
- Date: 2026-11-19

## 決定

- build 用 VM は、inputs を VM 内の作業ツリーと同じ path に置いて `run` を走らせ、template を save する前にその path を消す
- cache 層が VM 内の作業ツリーの中に作ったものは、template に残らない。作業ツリーの中に要るものは init で作る

## 根拠

- 残すと、本番の VM の repo の投入が、template に既にある path へ重なる。clone は空でないディレクトリに当たり、mount は中身を隠すので、同じ宣言でも投入方式で VM の中身が変わる
- inputs を host と同じ path に置けば、`run` が path を記録する tool（version manager の trust など）も本番の VM と同じ path で動く

## 却下した代替案

- 作業ツリーの中の成果物も残し、投入を重ねる: 投入方式ごとに結果が違い、clone では作れないおそれがある
- inputs を作業ツリーとは別の path に置く: `run` が見る path が本番の VM と食い違う

## 見直し条件

置かない（境界の置き方の決定で、実行の記録から測れる量が無い）
