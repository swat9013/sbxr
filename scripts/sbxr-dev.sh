#!/usr/bin/env bash
set -euo pipefail

# この script がある checkout の sbxr を build し直してから、渡した引数で実行する。
# 開発中のコードを、実際の repo を相手にリリース前に試すためのもの。

root=$(cd "$(dirname "$(readlink -f "$0")")/.." && pwd)
bin="$root/dist/sbxr-dev"
go build -C "$root" -o "$bin" ./cmd/sbxr
exec "$bin" "$@"
