# 0018. 埋め込み kit を Kit spec v3 に書き直し、対応する sbx の下限を v0.51.0 にする

- Status: Accepted
- Date: 2026-11-16

## 決定

- 埋め込み kit `sbxr-boot`・`sbxr-herdr` を Kit spec v3（`schemaVersion: "3"`、`lifecycle@1`）に書き直し、v2 の kit は消す
- v2 と v3 を sbx の版で切り替える分岐は作らない
- sbxr が対応する sbx の下限を v0.51.0 にする
- system.md の「未実測の前提」にあった「移行は spec が成熟するまで実装しない」は、この決定で置き換える

## 根拠

- sbx v0.51.0 は built-in agent を公開の v3 kit に解決し、v1・v2 の kit を v3 の kit と混ぜることを拒む。sbx v0.53.0 では `sbxr create` が VM を作る段で失敗する（system.md の 2026-11-16 の実測）
- v3 の kit でも、startup log の場所と dispatcher の行の形は変わらない（同じ実測）。Sbx adapter が kit startup の完了と失敗を読む形は変えずに済む
- v2 の kit を使えるのは sbx v0.50.x 以下だけで、そこでは built-in の image が更新されない。互換を保って守る利用者がいない

## 却下した代替案

- v2 と v3 の両方を持ち、sbx の版で切り替える: kit と adapter の経路が 2 本になり、実 sbx での確認を 2 つの版で通し続けることになる
- sbx を v0.50.x 以下に固定する: built-in の image と sbx 自身の修正を受け取れなくなる
- spec の最終版を待つ: 待つ間 create が使えない。spec は prerelease（`v3.0.0-m.12`）なので、後の版で descriptor が変われば kit を直し直す。その費用は受け入れる

## 見直し条件

置かない（引き金は sbx と spec の release という repo の外の出来事で、実行の記録から測れる量が無い。版のずれは doctor が検査する。decision/0019）
