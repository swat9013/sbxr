# 埋め込み資材は sandbox VM ごとの状態ディレクトリへ書き出す

Status: accepted (2026-09-25)

`sbx env create` と `sbx env rm` には同じ実在のディレクトリを渡す必要がある（lifecycle が相対 path でコマンドを呼ぶため）。single binary に埋め込んだ資材（env 定義と kit）は、create 時に `${XDG_STATE_HOME:-~/.local/state}/sbxr/sandboxes/<name>/` へ書き出し、destroy もそこを使って最後に消す。ここには作成時に確定した宣言も残し、drift 検出の基準にする。lifecycle から呼ぶ処理は `sbxr` の隠しサブコマンドにし、書き出す資材を減らす。

## Considered Options

- CLI の版ごとの共有ディレクトリ（`~/.local/share/sbxr/<version>/`）: create と destroy の間に sbxr を更新すると path が変わり、`env rm` が壊れる
- 一時ディレクトリ: destroy 時に存在しない
