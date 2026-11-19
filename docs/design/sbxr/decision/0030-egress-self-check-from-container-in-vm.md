# 0030. egress 自己検証は、VM の中の container からも 1 往復ずつ確かめる

- Status: Accepted
- Date: 2026-11-19
- 覆す条件: VM の中の container の既定の通信が、VM 直下と同じ proxy の経路（`forward`）を通るようになったとき。または、container の中からの拒否の形（curl の exit 6・35・52）が変わったとき。実測は sbx v0.53.0・VM の Docker 29.10.0（[system.md の未実測の前提](../system.md#未実測の前提) 17 の 2026-11-19 の実測。#135）

## 決定

[system.md の未実測の前提](../system.md#未実測の前提) 17 の分岐は、「別の経路を通る」の側に置く。proxy の `transparent`（VM の中の container の既定の通信）と `forward`（VM 直下の通信と、[decision/0006](0006-egress-self-check-from-vm.md) の egress 自己検証）を別の経路とみなし、egress 自己検証に VM の中の container からの確認を足す。

- **確かめること**: 0006 の 2 つ（許可先に届く・許可外に届かない）を、VM 直下に加えて、VM の中の container からも 1 往復ずつ確かめる。宛先は VM 直下と同じ（[decision/0007](0007-egress-self-check-verdict-by-proxy-denial.md) の選び方）。probe 先が無い側を省くのも同じ
- **container の作り方**: VM の中の Docker で image `curlimages/curl:8.11.1`（#135 の実測で使った tag）を pull し、既定の network の container で、VM 直下と同じ probe の script を走らせる。container には proxy の環境変数を渡さない（container の既定の通信のまま）
- **判定**: VM 直下の判定（0007 の表と、[decision/0016](0016-proxy-denial-is-approval-pending-or-local-rule-block.md) の proxy の拒否応答）に、container の側だけ 1 行を足す

| 結果 | container からの許可先への probe | container からの許可外への probe |
|---|---|---|
| proxy の拒否応答 | 届かない（失敗） | 届かない（通る） |
| curl の exit 6・35・52（`transparent` の拒否: 名前解決の拒否・TLS の切断・空の応答） | 届かない（失敗） | 届かない（通る） |
| それ以外の HTTP 応答（状態コードを問わない） | 届く（通る） | 届いた（失敗） |
| それ以外の curl の失敗 | 届かない（失敗） | 確かめられない（失敗） |

- **image を pull できないとき**: container の確認が成り立たなかったものとして、egress 自己検証の失敗と同じ扱いで止まる（VM を残し、作成時の宣言を書かない）。理由には、VM の中の Docker が Docker Hub から pull するのに egress の `docker-registry` group が要ることを示す
- **順序**: VM の中で agent と Docker が使えることの確認（[decision/0027](0027-create-and-start-check-inside-vm.md)）を、egress 自己検証の前に置く。egress 自己検証は VM 直下 → container の順に行い、VM 直下が通らなければ container は試さない
- **create だけで確かめる**: start（UC10）では確かめない。VM 直下の egress 自己検証も create だけである

この 2 点（image を pull して使うこと、start では確かめないこと）は、#149 の triage が決めた。

## 根拠

- 自己検証が `forward` だけを通すと、`transparent` の側が壊れても create が成功を返す。隔離は優先順で勝つ目的である（decision/0023）
- #135 の実測では、container の既定の通信は proxy の環境変数も proxy の CA も持たず、`transparent` を通った。拒否は proxy の拒否応答にならず、名前解決の失敗（exit 6）か接続の切断（exit 35・52）で終わった。拒否の形を分けるのは container か VM 直下かではなく proxy を使うかどうかなので、container の側では proxy を使わない形の拒否を「届かない」と読む
- 0007 が却下した「curl が失敗したら届かないとみなす」は、proxy を通らない経路の失敗でも通ってしまうことが理由だった。sbx v0.53.0 では proxy を使わない通信も `transparent` で同じ global rule に判定される（#135）。それでも timeout や接続の拒否のような経路の不調は通さないよう、「届かない」と読むのは実測で拒否の形だった exit 6・35・52 だけにする
- image を pull するのは、#142（PR #147）が default の `docker-registry` group に Docker Hub の blob の CDN（`production.cloudfront.docker.com:443`）を足し、default の宣言のもとで VM の中の pull が通るようになったから
- tag を固定するのは、image の中の curl と shell が変わって probe の script の出力の形が変わるのを避けるため
- Docker の確認を先に置くのは、Docker を使えない VM で、container の確認が image の pull の失敗として（`docker-registry` group の案内とともに）示されるのを避けるため
- start で確かめないのは、container の通信の経路を決めるのは VM と sbx の proxy で、起こし直しでは変わらないから。global rule の変更は UC5 が揃える。start のたびに pull が起きる費用も避ける
- create のたびに Docker Hub から image を pull することになる。pull の所要と、pull の失敗（Docker Hub に届かない・rate limit）が create の成否に入る。隔離を確かめられないまま create を成功させるより、止めるほうを選ぶ（decision/0023 の優先順）

## 却下した代替案

- 同じ経路とみなして確かめない: `transparent` の側が壊れても気付けない
- 実行基盤の選び方（ADR 0005）を見直す: #135 の実測では、どの経路でも許可外に届かなかった。経路が 2 つあることは、確かめる対象を増やせば足りる
- pull を伴わない確かめ方（VM の中の file から image を作る）: VM に静的に link した curl があるかは確かめておらず、無ければ成り立たない
- start でも確かめる: 経路が変わる根拠が無く、毎回の pull の費用に見合わない
- container からの拒否も proxy の拒否応答だけで判定する（container に proxy を明示する）: proxy を明示すると `forward` を通り、確かめたい `transparent` を通らない
- container の側で curl の失敗をすべて「届かない」とみなす: timeout や接続の拒否のような経路の不調でも通ってしまう

## 見直し条件

- create が image の pull で止まった報告（Docker Hub に届かない・rate limit）が出たとき、pull を伴わない確かめ方か、pull の失敗を止めずに示すだけにする扱いを見直す
