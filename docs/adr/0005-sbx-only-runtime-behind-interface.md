# 実行基盤は sbx に固定し、境界だけを interface に置く

Status: accepted (2026-09-25)

sandbox VM の作成・破棄・egress rule の適用は `internal/runtime` の interface の裏に置くが、実装は sbx（Docker Sandboxes）だけにする。2 つ目の実装が無い段階で抽象を広げると interface の形が sbx に引きずられたまま固まるため、境界の位置だけを先に決める。sbx への引数は素通しせず、CLI が必要なフラグ（`--force` / `--yes` 等）を明示的に定義して adapter が変換する。

## Consequences

- Linux の対象は sbx が公式に対応する範囲（Ubuntu 24.04 以上 + KVM）に限られる。Amazon Linux 2023 などは、別の実行基盤（例: microsandbox）の実装を足すまで対象外
