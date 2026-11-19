# 実行基盤は sbx に固定し、境界だけを interface に置く

Status: accepted (2026-09-25)

sandbox VM の作成・破棄・egress rule の適用は `internal/runtime` の interface の裏に置くが、実装は sbx（Docker Sandboxes）だけにする。2 つ目の実装が無い段階で抽象を広げると interface の形が sbx に引きずられたまま固まるため、境界の位置だけを先に決める。sbx への引数は素通しせず、CLI が必要なフラグ（`--force` / `--yes` 等）を明示的に定義して adapter が変換する。

## Consequences

- Linux の対象は sbx が公式に対応する範囲（Ubuntu 24.04 以上 + KVM）に限られる。Amazon Linux 2023 などは、別の実行基盤（例: microsandbox）の実装を足すまで対象外

## 改訂（2026-09-27、#48）

test 用に、in-memory の Runtime adapter を足す。

- 本文の「実装は sbx だけ」は本番の実行基盤を指す。in-memory の adapter は test でだけ使い、本番の CLI には組み込まない
- domain の test（egress・secret・sandbox）は in-memory の adapter を使い、sbx の引数を見ない。sbx の引数と出力の扱いは、Sbx adapter の test が sbx stub の上で確かめる
- 2 つの adapter は同じ契約 test を通す。契約は、sbx の実測に基づく振る舞い（sandbox スコープの secret は VM の作成前に置ける、sandbox スコープ rule は作成後にしか置けない、VM が無くても env 定義の撤去は成功し secret を消す、など）を含む
- Runtime の interface に、VM のファイルを書く・読む・あるかを見る操作と、herdr が繋ぐ ssh target を置く。VM への書き込みの script（`sbx cp` が host の uid のまま置く癖への対処。ADR 0006）は Sbx adapter の内側に置く
- VM の状態は型付きにし、sbx の status の解釈は Sbx adapter が決める。状態を得られなかったときは、無いとも止まっているとも読ませない値を返す（destroy を許す側に倒さないため）
- VM のファイルの書き込みは、mode を変えない指定を持つ（settings.json は sbx が置いた mode を保つ）
- interface が「作る内容」を domain の言葉で受け取り、kit・startup log・順序の制約を adapter に持たせることは [decision/0009](../design/sbxr/decision/0009-runtime-receives-sandbox-spec.md) に書いた

## 改訂（2026-11-19）

実行基盤に sbx を選ぶ理由と、調べて採らなかった候補を書き足す。

- sbx は sandbox VM ごとに専用の Docker Engine を持ち、VM の中で agent が Docker を使える。プロセス単位の隔離（OS-level sandbox）では、Docker を使えるようにすると Docker 経由で制限を抜けられた。VM 単位の隔離の中に Docker を置けることが、sbx を選ぶ理由である（[decision/0023](../design/sbxr/decision/0023-purpose-isolation-development-ease.md)）
- 別の実行基盤の候補として microsandbox（v0.7.16）を調べ、採らないと決めた
  - VM の中で Docker は動くが、標準の機能ではなく、image と起動の仕組みを自前で持つ必要がある
  - VM の起動ごとに走る処理を宣言する仕組みが見当たらない（boot の再生と herdr の起動が立つ仕組み）
  - egress の許可を迂回できる不具合の advisory が、2026-09〜10 に複数出ている
  - 調べたのは docs・source・release の記述で、実機では確かめていない
- Consequences の「別の実行基盤（例: microsandbox）の実装を足すまで対象外」は、microsandbox を足す予定があることを意味しない
