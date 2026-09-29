# Go 製 CLI をローカルでデバッグする: 実践の整理

調査日: 2026-09-29。

根拠は一次情報に限った。Go の文書・`go help`・`go doc`・Go 本体の source、Delve の文書と source、vscode-go の文書である。「ローカルで確認」と書いた主張は、次の環境で実際に実行して確かめた。

- Go: go1.27.1 darwin/arm64
- Delve: v1.27.2。`go install github.com/go-delve/delve/cmd/dlv@latest` で作業用ディレクトリに入れた。この Mac の PATH には `dlv` が無い
- debugserver: `lldb-2103.0.34.103`（Xcode Command Line Tools 同梱）
- macOS: 26.6.2（Darwin 25.6.0）。ただし **SIP は無効（`csrutil status`）で、Developer mode も無効（`DevToolsSecurity -status`）**。SIP を有効にした標準の Mac で同じ結果になるかは確かめていない

go.dev の文書は正規の URL で引用した。本文は [golang/website](https://github.com/golang/website) の source（`gh api` で取得）か、手元の `$GOROOT/doc` で照合した。

## 要点

1. **デバッグ用の binary は `go build -gcflags=all="-N -l"` で作る。**
   - `go run` は `-s -w` で link するので DWARF（デバッガが読む情報）が無い（go1.27.1 の source と `go help run`）。
   - release の binary（`-ldflags "-s -w"`）も同じ理由でデバッグできない。
   - `dlv debug` と `dlv test` は、`-N -l` を自分で付けて build する。
2. **y/N の確認のように端末から読む CLI は、`dlv debug`/`dlv exec` の対話 client から起動してはいけない。**
   - 対話 client から起動すると、target の stdin は端末と判定される。それでも、stdin は起動した端末につながっていない（probe でローカルで確認。詳細は 2 章）。
   - 次のどれかを使う。
     - `--headless` にして、別の端末から `dlv connect` する
     - VS Code で `"console": "integratedTerminal"` にする
     - 実行中の process に `dlv attach` する
     - 確認を省く flag を使う
   - `-r stdin:file` を使うと stdin は端末でなくなる。そのため、端末かどうかを確かめる処理に弾かれる。
3. **副作用は環境変数で一時ディレクトリに向ける。test では `t.TempDir`・`t.Setenv` を使う。**
   - 外部コマンドは次の順で差し替える。上ほど軽い。
     - 関数を注入する
     - test binary 自身を子 process として再実行する（今の `os/exec` の test の方式）
     - PATH 上に stub を置く
     - testscript
4. **デバッガを使わずに観測する手段も用意しておく。** 短命な CLI では、デバッガより安いことが多い。
   - debug level を環境変数で切り替える `log/slog`
   - `GOTRACEBACK`
   - `Ctrl-\`（SIGQUIT）による stack dump
   - profile と trace は `os.Exit` の前に止める。`os.Exit` は defer を実行しないため。
5. **macOS では、Delve は既定で Xcode CLT の `debugserver` を使う。**
   - dlv の codesign（コード署名）は要らない。要るのは native backend だけ。
   - 要るのは `xcode-select --install` と、必要に応じた `DevToolsSecurity -enable` である。
   - Rosetta 上の dlv は起動を拒否される。
6. **`-race` は実行された経路の data race だけを見つける。**
   - 目安として、memory は 5〜10 倍、実行時間は 2〜20 倍になる。
   - 日常のデバッグでは常用しない。test と、疑わしい手動実行に使う。

## 1. デバッグ用に build する

### `go run` と `go build`

- **`go run` の binary にはデバッガ用の情報が無い。**
  - `go help run` の記述: "By default, 'go run' compiles the binary without generating the information used by debuggers, to reduce build time. To include debugger information in the binary, use 'go build'."（go1.27.1 でローカルで確認）
  - 実装では、`go run` が `OmitDebug = true` を立てる（[run.go#L151](https://github.com/golang/go/blob/go1.27.1/src/cmd/go/internal/run/run.go#L151)）。すると linker に `-s -w` が渡る（[gc.go#L605](https://github.com/golang/go/blob/go1.27.1/src/cmd/go/internal/work/gc.go#L605)）。
  - `-w` は "disable DWARF generation"、`-s` は "disable symbol table" である（`go tool link -help`。ローカルで確認。[cmd/link](https://pkg.go.dev/cmd/link)）。
- **`go run` は VCS の情報を binary に刻まない。**
  - VCS の情報を自動で読むのは `go build`・`go install`・`go list` だけである（`AutoVCS: true`。[build.go#L472](https://github.com/golang/go/blob/go1.27.1/src/cmd/go/internal/work/build.go#L472)、[#L701](https://github.com/golang/go/blob/go1.27.1/src/cmd/go/internal/work/build.go#L701)、[load/pkg.go の判定](https://github.com/golang/go/blob/go1.27.1/src/cmd/go/internal/load/pkg.go#L2547)）。
  - sbxr で試した結果（ローカルで確認）:
    - `go run ./cmd/sbxr --version` は `sbxr version (devel)` を出す
    - `go build` した binary は `sbxr version v0.2.1-0.20260929014419-58f09076106b` を出す

### `-gcflags=all="-N -l"`

- `-N` は "disable optimizations"、`-l` は "disable inlining" である（`go tool compile -help`。ローカルで確認。[cmd/compile](https://pkg.go.dev/cmd/compile)）。
- `all=` を付けると、依存 package にも同じ flag が効く。`-gcflags=-S fmt` は fmt だけに効き、`-gcflags=all=-S fmt` は依存にも効く（`go help build`）。
- `dlv exec` の help は次のように勧めている。"if the binary was not compiled with optimizations disabled, it may be difficult to properly debug it. Please consider compiling debugging binaries with -gcflags="all=-N -l""（[dlv_exec.md](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/usage/dlv_exec.md)、`dlv help exec`。ローカルで確認）
- `dlv debug`/`dlv test` は、自分で `-gcflags all=-N -l` を付けて build する（[gobuild.go#L83](https://github.com/go-delve/delve/blob/v1.27.2/pkg/gobuild/gobuild.go#L83)）。
- flag は build info に残る。`go version -m` で、`build -gcflags="all=-N -l"` が記録されていることを確かめた（ローカルで確認）。

### `-race`

- **何を見つけるか:** 実行時に実際に起きた data race だけを見つける。"The race detector only finds races that happen at runtime, so it can't find races in code paths that are not executed." 文書は、まず `go test -race` で走らせ、test の coverage が足りなければ `-race` 付きの binary を現実に近い負荷で動かすよう勧めている（[Data Race Detector](https://go.dev/doc/articles/race_detector)）。
- **代償:** "memory usage may increase by 5-10x and execution time by 2-20x"（同上。Runtime Overhead の節）。
- **条件:**
  - cgo が要る。Darwin 以外では C compiler も要る（同上。Requirements の節）。
  - darwin/arm64 は対応している（`go help build`。ローカルで確認）。
- **調整:** `GORACE` で調整できる。`halt_on_error`（最初の race で終了する）、`exitcode`（既定は 66）、`log_path` がある（同上）。

### `-trimpath`

- 動作: "remove all file system paths from the resulting executable"。記録される file 名は `module@version` か import path になる（`go help build`。ローカルで確認）。
- デバッグへの影響: Delve は、binary に埋め込まれた source の path を使う。そのため `-trimpath` を付けると、source の表示と path 指定の breakpoint が合わなくなる。直すには `substitutePath`（CLI では `config substitute-path`）を設定する（[Delve FAQ: substpath](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/faq.md#substpath)）。
- 使い分け: release の binary には付ける。デバッグ用には付けない。

### build info と `--version`

- `debug.ReadBuildInfo` は "the build information embedded in the running binary" を返す。これは module を使った build にだけある（[runtime/debug.ReadBuildInfo](https://pkg.go.dev/runtime/debug#ReadBuildInfo)）。
- Go 1.24 から、`go build` は main module の版を VCS の tag・commit から決めて、`BuildInfo.Main.Version` に入れる。未 commit の変更があれば `+dirty` が付く。`-buildvcs=false` で外せる（[Go 1.24 release notes](https://go.dev/doc/go1.24#go-command)）。
- `BuildInfo.Settings` には `vcs.revision`・`vcs.time`・`vcs.modified`・`-gcflags` などが入る（[runtime/debug.BuildSetting](https://pkg.go.dev/runtime/debug#BuildSetting)）。これを `--version` に出せば、「いま動いている binary がどの commit で、どの flag で build されたか」を報告から判別できる。

## 2. Delve

### 起動の 4 形態

- **`dlv debug [package]`:** package を build して起動する。
  - binary は、cwd に `os.CreateTemp(".", "__debug_bin")` で作る（[commands.go#L633](https://github.com/go-delve/delve/blob/v1.27.2/cmd/dlv/cmds/commands.go#L633)、[defaultexe.go](https://github.com/go-delve/delve/blob/v1.27.2/pkg/gobuild/defaultexe.go)）。
  - `--output` で置き場を変えられる。
- **`dlv exec <binary>`:** build 済みの binary を起動する（[dlv_exec.md](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/usage/dlv_exec.md)）。
- **`dlv test [package] -- -test.run TestX -test.v`:** test binary をデバッグする（[dlv_test.md](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/usage/dlv_test.md)）。
- **`dlv attach <pid> [executable]`:** 実行中の process に入る。
  - `--waitfor <prefix>` で、名前がその prefix で始まる process の起動を待てる（[dlv_attach.md](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/usage/dlv_attach.md)）。
  - vscode-go の文書は、local の attach 先も `-gcflags=all="-N -l"` で build するよう求めている（[vscode-go debugging.md](https://github.com/golang/vscode-go/blob/7ce26ceacc5b29185ae15025705c654a2969aadc/docs/debugging.md)）。
- **CLI の引数:** `--` の後に置く。"Pass flags to the program you are debugging using `--`, for example: `dlv exec ./hello -- server --config conf/config.toml`"（`dlv help`。[dlv.md](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/usage/dlv.md)）。

ローカルで確認した例:

```
dlv exec ./sbxr-dbg --init init.dlv -- --version
```

- `init.dlv` には `break main.resolveVersion` と `continue` を書いた。
- `main.resolveVersion` で止まり、引数 `linkedVersion = ""` を読めた。
- `--init` の file はデバッガの command を順に実行する。
- `exit -c` は `--accept-multiclient` の server に接続していないと失敗する（ローカルで確認）。そのため、init file の終わりには `exit` を使う。

### headless と `--accept-multiclient`

- **`--headless`:** "Run debug server only"。JSON-RPC と DAP の両方の client を受ける（`dlv help`）。
- **起動直後の停止:** program は接続して `continue` するまで動かない。すぐ走らせたいときは `--continue --accept-multiclient` を付ける（[FAQ: docker](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/faq.md#docker)）。`--continue` 単独は `Error: --continue requires --accept-multiclient` で止まる（[commands.go#L1086](https://github.com/go-delve/delve/blob/v1.27.2/cmd/dlv/cmds/commands.go#L1086)）。
- **`--accept-multiclient`:** 接続先の server が client の切断後も残り、何度でも接続できるようになる。VS Code の `request: attach`・`mode: remote` からも、`dlv connect` からもつなげる（[vscode-go debugging.md](https://github.com/golang/vscode-go/blob/7ce26ceacc5b29185ae15025705c654a2969aadc/docs/debugging.md)）。
- **安全性:**
  - 接続は認証されず、任意のコードを実行できる（[FAQ: docker](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/faq.md#docker)）。
  - 既定の listen 先は `127.0.0.1:0` で、`--only-same-user` も既定で有効である（`dlv help`。ローカルで確認）。
  - `--listen :PORT` のように全 interface で待たない。

### stdin を読む CLI、TTY が要る CLI

Delve の FAQ は、CLI をデバッグする方法として 3 つを挙げている（[FAQ: ttydebug](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/faq.md#ttydebug)）。

- 別の端末で CLI を起動し、`dlv attach` する
- `dlv debug --headless` で起動し、別の端末から接続する。この方法では "place the process in the foreground and allow it to access the terminal TTY"
- `--tty` で process に専用の TTY を与える。"For the best experience, you should create your own PTY"

source で確かめた仕組みは次のとおり。

- **foreground になる条件:** target を foreground にするのは、`headless && tty == ""` のときだけである（[commands.go#L1162](https://github.com/go-delve/delve/blob/v1.27.2/cmd/dlv/cmds/commands.go#L1162)）。`dlv dap`（VS Code が使う）は常に `Foreground: true` である（[commands.go#L593](https://github.com/go-delve/delve/blob/v1.27.2/cmd/dlv/cmds/commands.go#L593)）。
- **foreground のときの処理（macOS の lldb backend）:**
  - debugserver の stdin に dlv の stdin をつなぐ。
  - `tcsetpgrp` で、target を端末の前面の process group にする（[gdbserver.go の LLDBLaunch](https://github.com/go-delve/delve/blob/v1.27.2/pkg/proc/gdbserial/gdbserver.go#L462)）。
- **`--tty` を指定したとき:** debugserver に `--stdio-path <tty>` を渡す。このとき `-r` の指定は使われない（同じ関数の分岐）。

ローカルで確認した挙動は次の表のとおり。確かめ方は次のとおり。

- probe を作った。stdin が端末かどうかを `golang.org/x/term.IsTerminal` で調べ、1 行読む program である。
- それを `script -q /dev/null` の pty の下で動かし、`y\n` を流し込んだ。

| 起動の仕方 | `IsTerminal(stdin)` | 読めたもの |
|---|---|---|
| 直接実行 | true | `y` は読めず、端末の EOF（`script` が流す ^D）を受けた |
| `dlv exec probe`（対話 client） | **true** | **何も届かない（5 秒で timeout）** |
| `dlv exec --headless --continue --accept-multiclient probe` | true | 直接実行と同じく EOF を受けた |
| `dlv exec -r stdin:answer.txt probe` | **false** | file の中身 `y\n` を読んだ |

流し込んだ `y` は、直接実行でも読めなかった。したがってこの実験が示すのは、「target の stdin が、起動した端末（pty）につながっているか」までである。打った答えが届くことまでは示していない。

この表から次のことが言える。

- **対話 client の下では、stdin は端末と判定されるのに、起動した端末とはつながっていない。** 端末かどうかを確かめる処理は通り、その後の読み込みで待ち続けることになる。debugserver が target 用に端末を別に用意しているためと読める。ただし、これは推測で、debugserver の source は確かめていない。
- **headless では、target の stdin は起動した端末につながる。** source の `Foreground`・`tcsetpgrp` の処理とも合う。
- **stdin が端末であることを前提にする処理には、`-r` は使えない。** stdin が file になり、端末でないと判定されるため。
- **端末が要る処理に使えるのは次の 4 つである。**
  - headless で起動し、別の端末から `dlv connect` する
  - `--tty` で専用の端末を与える
  - `attach` する
  - VS Code の integratedTerminal で起動する

**`--tty` の制約:**

- FAQ は、専用の PTY を作って `--tty` に渡すよう勧めている（[ptyme](https://github.com/derekparker/ptyme)）。
- 別の端末の `tty` の出力（例 `/dev/ttys003`）を渡すと、その端末の shell も同じ端末から読むので、入力を取り合うはずである。これは推測で、FAQ はこの理由を書いていない。
- `--tty` は `dlv debug` と `dlv exec` の flag である（`dlv help debug`・`dlv help exec`。ローカルで確認）。`dlv test` と `dlv attach` には無い。

## 3. エディタとの統合

### VS Code（vscode-go）

以下の出典は [vscode-go docs/debugging.md（commit 7ce26ce、2026-07-08）](https://github.com/golang/vscode-go/blob/7ce26ceacc5b29185ae15025705c654a2969aadc/docs/debugging.md) である。

- **`mode`:** launch の値は `auto`・`debug`・`test`・`exec`・`replay`・`core` である。
  - `exec` は `program` に build 済みの binary を指す。その binary は `-gcflags=all="-N -l"` で build しておく。
  - `test` の `args` には `-test.run` などを渡す。
- **引数と環境:** `args` は program への引数、`env` は環境変数、`envFile` は `KEY=VALUE` の file、`buildFlags` は build の flag である。
- **stdin:** 既定では "does not handle STDIN" である。STDIN や TTY が要る program には、`"console": "integratedTerminal"`（または `"externalTerminal"`）を使う。拡張は、DAP の `RunInTerminal` で端末を VS Code に任せる。
  - `console` は **(Experimental)** と明記されている。
  - remote debugging では無視される。
  - attach に付けても "does not affect tty of the running program" である。
- **attach:** `request: attach`・`mode: remote` で、`dlv ... --headless --listen=:PORT --accept-multiclient` に接続する。

sbxr 向けの例（repo には置いていない）:

```jsonc
{
  "name": "sbxr create (fixture)",
  "type": "go",
  "request": "launch",
  "mode": "debug",
  "program": "${workspaceFolder}/cmd/sbxr",
  "args": ["create", "/tmp/sbxr-work/sbxr-fixture"],
  "env": {
    "XDG_STATE_HOME": "/tmp/sbxr-work/state",
    "XDG_CACHE_HOME": "/tmp/sbxr-work/cache"
  },
  "console": "integratedTerminal"
}
```

### GoLand

GoLand にも、Delve を使う run/debug configuration がある（[GoLand: Debugging code](https://www.jetbrains.com/help/go/debugging-code.html)）。**未検証:** jetbrains.com には今回の環境から到達できず、中身を確かめていない。

## 4. デバッグ中の副作用を隔離する

- **XDG_\* で一時ディレクトリに向ける。**
  - `os.UserHomeDir` は、macOS を含む Unix では `$HOME` を返す（[os.UserHomeDir](https://pkg.go.dev/os#UserHomeDir)）。
  - `$HOME` から path を決める CLI は、`HOME` を変えると隔離できる。
  - ただし、呼び出す外部コマンドも同じ `HOME` を見る。外部コマンドの認証まで外れることがあるので、実物に対して試す手動実行では、個別の XDG_\* を使う。
- **`t.TempDir()`:**
  - test ごとに固有のディレクトリを返し、test とその subtest が終わると消す。
  - `GOTMPDIR` があれば、その下に作る（[testing.T.TempDir](https://pkg.go.dev/testing#T.TempDir)）。
- **`t.Setenv(key, value)`:**
  - 環境変数を設定し、Cleanup で元に戻す。
  - process 全体に効くので、parallel test では使えない（[testing.T.Setenv](https://pkg.go.dev/testing#T.Setenv)）。
  - cwd に依存するなら、Go 1.24 で入った `t.Chdir` がある。これにも同じ制約がある（[testing.T.Chdir](https://pkg.go.dev/testing#T.Chdir)、[Go 1.24 release notes](https://go.dev/doc/go1.24)）。
- **testscript（`github.com/rogpeppe/go-internal/testscript`、v1.16.0）:**
  - 由来: go-internal は、testscript を "Extracted from the core Go team's internal testscript package (cmd/go/internal/script), which is heavily used to test the `go` command" と説明している（[go-internal README](https://github.com/rogpeppe/go-internal#readme)）。
    - go1.27.1 の cmd/go は、script test の engine を今は `cmd/internal/script` に置いている。`src/cmd/go/testdata/script/` には 929 件の script がある（ローカルで確認）。
    - 「cmd/go が testscript を使う」というより、「testscript は cmd/go の script test から切り出された」が正確である。
  - 実行のされ方: `testscript.Main(m, map[string]func(){...})` で登録したコマンドは、PATH 上に置かれ、**別 process として**実行される（[exe.go#L40](https://github.com/rogpeppe/go-internal/blob/v1.16.0/testscript/exe.go#L40)）。
  - 各 script の環境は `$WORK`（一時ディレクトリ）の下に作られ、`HOME=/no-home`・`TMPDIR=$WORK/.tmp` が入る。`stdin file` で次の `exec` の stdin を file にできる（[testscript doc.go](https://github.com/rogpeppe/go-internal/blob/v1.16.0/testscript/doc.go)）。
  - 結果として、stdin は端末にならない。
  - `RunMain` は非推奨で、`Main` を使う（同 exe.go）。
- **`os/exec` が起動する外部コマンドを fake にする方法:**
  - **test binary 自身を再実行する（今の形）:**
    - go1.27.1 の `os/exec` の test は、`TestMain` で分岐する。環境変数 `GO_EXEC_TEST_PID` があれば、test を走らせずに、引数で指定された helper コマンドとして振る舞う。
    - 子 process は `testenv.Executable(t)` で test binary 自身を起動する（[exec_test.go#L73](https://github.com/golang/go/blob/go1.27.1/src/os/exec/exec_test.go#L73)、[helperCommandContext #L158](https://github.com/golang/go/blob/go1.27.1/src/os/exec/exec_test.go#L158)）。
    - 登録した helper が使われていなければ失敗させる仕組みもある（[registerHelperCommand #L136](https://github.com/golang/go/blob/go1.27.1/src/os/exec/exec_test.go#L136)）。
  - **`TestHelperProcess`（旧い形）:**
    - `GO_WANT_HELPER_PROCESS=1` を付けて、`os.Args[0] -test.run=TestHelperProcess -- <cmd>` を起動する形である。
    - go1.18 までの `os/exec` の test にあった（[go1.18 exec_test.go#L681](https://github.com/golang/go/blob/go1.18/src/os/exec/exec_test.go#L681)）。
    - go1.19 以降の同じ file には無い（`gh api` で確認）。
  - **PATH 上の stub:**
    - `exec.Command` と `LookPath` は PATH からコマンドを探す。
    - 相対 path の entry で見つかったものは `ErrDot` で拒否される（[os/exec](https://pkg.go.dev/os/exec)）。
    - したがって、stub を置く dir は絶対 path（`t.TempDir()`）にして、`t.Setenv("PATH", dir+":"+os.Getenv("PATH"))` とする。

## 5. デバッガを使わずに観測する

- **`log/slog` の level を実行時に切り替える。**
  - `HandlerOptions.Level` に `*slog.LevelVar` を渡すと、level を実行時に変えられる（[log/slog](https://pkg.go.dev/log/slog)、[HandlerOptions](https://pkg.go.dev/log/slog#HandlerOptions)）。
  - `Level.UnmarshalText` は `DEBUG` などを大小文字を問わず読む（[Level.UnmarshalText](https://pkg.go.dev/log/slog#Level.UnmarshalText)）。
  - そのため、環境変数の値をそのまま level にできる。例: `SBXR_LOG=debug` → `LevelVar.UnmarshalText`。
  - 出力先は stderr にし、stdout の出力（機械が読む出力）を汚さない。
- **`GODEBUG`:**
  - 書式は `key=value` の comma 区切りで、知らない設定は無視される（[Go, Backwards Compatibility, and GODEBUG](https://go.dev/doc/godebug)。`$GOROOT/doc/godebug.md` で照合）。
  - runtime のデバッグ用の変数は [runtime の Environment Variables](https://pkg.go.dev/runtime#hdr-Environment_Variables) にある。
  - 短命な CLI で効くのは `inittrace=1` である。起動時の package の init にかかる時間と allocation を 1 行ずつ出す（`go doc runtime`。ローカルで確認）。
- **`GOTRACEBACK`:**
  - 既定の `single` は、panic した goroutine だけを出して exit code 2 で終わる。
  - `all` は、user が作った全 goroutine を出す。
  - `system` は、runtime の frame も出す。
  - `crash` は、Unix で SIGABRT を起こし、core dump を残す（[runtime](https://pkg.go.dev/runtime#hdr-Environment_Variables)）。
  - `debug.SetTraceback` を使えば、code から出力を増やせる（減らすことはできない）（[runtime/debug.SetTraceback](https://pkg.go.dev/runtime/debug#SetTraceback)）。
- **固まった CLI には `Ctrl-\` を押す。** SIGQUIT を受けた Go program は、stack dump を出して終わる（[os/signal](https://pkg.go.dev/os/signal)）。どこで待っているか（子 process の終了待ちか、入力待ちか）が、デバッガ無しで分かる。
- **pprof と trace:**
  - standalone の program では、`pprof.StartCPUProfile`/`StopCPUProfile` と、`pprof.Lookup("allocs").WriteTo` を main に足す（[runtime/pprof](https://pkg.go.dev/runtime/pprof)）。
  - trace は `runtime/trace` の Start/Stop で取る。trace は goroutine の block と syscall の出入りを記録する（[runtime/trace](https://pkg.go.dev/runtime/trace)）。
  - 子 process を待つ時間が長い CLI では、CPU profile より trace のほうが情報になる。
  - test からなら、`go test -cpuprofile`/`-memprofile`/`-trace` で済む（`go help testflag`。ローカルで確認）。
  - **注意:** `os.Exit` は "deferred functions are not run" である（[os.Exit](https://pkg.go.dev/os#Exit)）。main が error で `os.Exit(1)` する CLI では、profile や trace を defer で止めると、失敗した実行の分だけ出力が欠ける。止める処理は `os.Exit` の前に明示的に呼ぶ。

## 6. macOS 固有の注意

- **backend:**
  - Delve の既定の backend は、macOS では lldb である。"default: Uses lldb on macOS, native everywhere else"（`dlv help backend`。ローカルで確認）。
  - lldb backend は `debugserver` を探す。探す順は、環境変数 `DELVE_DEBUGSERVER_PATH`、PATH、`/Library/Developer/CommandLineTools/.../debugserver`、`xcode-select -p` の下の順である（[gdbserver.go#L98-L125](https://github.com/go-delve/delve/blob/v1.27.2/pkg/proc/gdbserial/gdbserver.go#L98)）。
  - 見つからなければ、`debugserver or lldb-server not found: install Xcode's command line tools or lldb-server` を出す（[debugger.go#L390](https://github.com/go-delve/delve/blob/v1.27.2/service/debugger/debugger.go#L390)）。
- **準備:**
  - `xcode-select --install` を実行する。
  - Developer Mode が無効だと、デバッガを使うたびに許可を求められる。`sudo /usr/sbin/DevToolsSecurity -enable` で、session ごとに 1 回で済む。
  - 必要なら、user を `_developer` group に入れる（[Delve install: macOS considerations](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/installation/README.md#macos-considerations)）。
  - この Mac では Developer mode が無効のままでも、`dlv exec` は許可を求めずに起動した（ローカルで確認）。ただし SIP も無効な機体なので、一般化はできない。
- **codesign:**
  - 自分で codesign が要るのは native backend だけである。README は "You do not need the macOS native backend and it has known problems" と書いている（同上）。
  - Delve の build script も、`CERT` を指定して native backend を build するときにだけ署名する（[_scripts/make.go](https://github.com/go-delve/delve/blob/v1.27.2/_scripts/make.go)）。
  - `go install` で入れた dlv を、そのまま lldb backend で使える。
- **Rosetta:** Rosetta の下で動く dlv（arm64 の Mac に amd64 の Go で入れたもの）は、"can not run under Rosetta, check that the installed build of Go is right for your CPU architecture" で止まる（[rosetta_darwin.go](https://github.com/go-delve/delve/blob/v1.27.2/pkg/proc/macutil/rosetta_darwin.go)）。
- **`DYLD_INSERT_LIBRARIES`:** Ventura 以降、debugserver に渡すと crash する。そのため Delve は、target の環境からこの変数を取り除く（[gdbserver.go#L559](https://github.com/go-delve/delve/blob/v1.27.2/pkg/proc/gdbserial/gdbserver.go#L559)）。
- **SIP:** **一次情報で確かめられなかった。** SIP で保護された system binary（`/usr/bin/*` など）に attach できない、という制約はよく知られている。しかし Apple の文書（developer.apple.com）には今回到達できず、Delve の文書と source にも SIP への言及は無い。
  - 自分で build した Go の binary をデバッグする限り、SIP は関係しない見込みである（推測）。

## sbxr への当てはめ

読んだ file は次のとおり。

- `cmd/sbxr/{main,prompt,version,sandbox,secret}.go` と、その test
- `internal/runtime/{sbx,sbx_test}.go`
- `internal/runtime/sbxstub/`
- `internal/herdr/herdr.go`
- `.goreleaser.yaml`
- `.github/workflows/ci.yml`
- `docs/real-sbx-walkthrough-macos.md`

| 実践 | sbxr に既にあるもの | 足りないもの・提案 |
|---|---|---|
| デバッグ用の build | walkthrough は `go build -o "$WORK/sbxr" ./cmd/sbxr` で build する（最適化あり） | 実 sbx に対してデバッガで追う手順が要るなら、walkthrough に `-gcflags=all="-N -l"` の変種と `dlv exec "$WORK/sbxr" -- plan "$FIXTURE"` を 1 行ずつ足す。release の binary は `.goreleaser.yaml` の `-s -w` で DWARF が無いので、Homebrew の sbxr はデバッグできない。再現は checkout から build する |
| `--version` と build info | `resolveVersion` は、ldflags、`BuildInfo.Main.Version`、`(devel)` の順に版を選ぶ（`version.go`） | `go build` でも版は入る（Go 1.24 以降。ローカルで `v0.2.1-0.2026…-58f09076106b` を確認）。`version.go` のコメント「go install 時に記録される」は、`go build` にも当てはまる。報告から binary を特定したいなら、`vcs.revision`・`vcs.modified` を `--version` に足す案がある。`go run` の出力は常に `(devel)` になることを知っておく |
| `-race` | CI が `go test -race -shuffle=on ./...` を macOS と Linux で走らせる | 既に足りている。`Hidden` の signal 用 goroutine は test で fake に置き換わるので、race 検査が届かない。手動の 1 周を `go build -race` の binary で回す案はある（優先度は低い） |
| `-trimpath` | `.goreleaser.yaml` は指定していない | release に付けるなら、dlv 側で `substitutePath` が要る。ただし release の binary はもともと `-w` でデバッグできないので、実際に困ることは無い |
| Delve の導入 | この Mac の PATH に `dlv` は無い | `go install github.com/go-delve/delve/cmd/dlv@latest`（v1.27.2 は go1.27.1 で動作を確認）。`dlv debug` は cwd に `__debug_bin*` を作る。正常に終われば消えるが、強制終了すると残る。`.gitignore` には入っていない |
| 端末の確認（`Confirm`/`Hidden`） | `Confirm`・`Hidden` は `term.IsTerminal(stdin)` を確かめる（`prompt.go`）。`create`/`destroy` には `--yes` がある（`sandbox.go`）。test は `fakePrompter` で置き換える | `dlv debug ./cmd/sbxr -- create <repo>` を対話 client で起動すると、`Confirm` は端末の確認を通る。しかし stdin は起動した端末につながっていないので、答えを待ったまま止まる見込みである（probe の結果からの推論。sbxr 本体では試していない）。確認の分岐を追うなら、headless と `dlv connect`、または VS Code の integratedTerminal を使う。そうでなければ `--yes` で省く。`secret setup github` / `secret setup custom` の `Hidden` には省く flag が無いので、端末を与える方法しか無い |
| エディタ | `.vscode/` は無い | 上の launch.json の例を、開発者の手元に置く（repo に入れるかは別に決める） |
| 副作用の隔離（手動） | `xdgDir` は、絶対 path の `XDG_STATE_HOME`/`XDG_CACHE_HOME` だけを使う（`sandbox.go`）。walkthrough は両方を `mktemp -d` に向ける | user 設定と secret は `~/.config/sbxr/` に固定である（ADR 0002 / 0004）。実 sbx は認証を `$HOME` の下に持つので、`HOME` も変えられない。手動のデバッグでは、walkthrough と同じく `~/.config/sbxr/` を一時的に退避する。上書き用の環境変数を足すかどうかは ADR 0004 に関わるので、ここでは提案しない |
| 副作用の隔離（test） | `t.TempDir`・`t.Setenv("XDG_STATE_HOME", …)`・`t.Setenv("HOME", …)` を既に使っている（`main_test.go`、`sandbox_test.go`） | 既に足りている |
| 外部コマンドの fake | `CommandRunner` を注入し、in-process の `sbxstub.Stub.Run` に置き換える。`runInsideLocally` は `sbx exec -- <args>` を host の sh で実行する（`sbx_test.go`） | `ExecSbx` 自体に test が無い（grep で確認）。その中身は、stderr を error に含めること、失敗しても stdout を返すこと、stdin の受け渡しである。`herdr.run`・`sandbox.ExecClone` にも無い。`os/exec` の test と同じ `TestMain` での再実行か、`t.TempDir()` の PATH stub で、「子 process が非 0 で終わり stderr を出す」場合を 1 本ずつ確かめられる |
| testscript | 無い。`acceptance_test.go` は cobra の root を in-process で、stub の上で動かす | 足りないのは、`main()` が依存を組み立てる部分と、exit code の経路である。testscript で `sbxr` と偽の `sbx` を `Main` に登録すれば覆える。ただし次の手間がかかる。<br>- `HOME=/no-home` なので、`Setup` で `HOME`・`XDG_*` を設定する<br>- stdin が端末でないので、`--yes` が必須になる<br>- `secret` 系は `Hidden` のせいで通せない<br>- 偽の `sbx` は process をまたいで状態を持つ必要がある（`sbxstub` は in-memory なので、file に書く版が要る）<br>費用に見合うかは、main の組み立てで起きた不具合の実績で決める |
| `log/slog` | 使っていない（grep で確認）。失敗は error の文言だけで伝わる | 例えば `SBXR_LOG=debug` で `slog.LevelVar` を上げ、`ExecSbx`/`herdr.run` の argv・所要時間・exit code を stderr に出す。secret の値は stdin で渡る設計なので、**stdin の中身は記録しない**。argv だけを出す |
| `GOTRACEBACK`・SIGQUIT | 特に無い | sbx の呼び出しが返らないときは、`Ctrl-\` で、どの `sbx` 呼び出しの `cmd.Output()` で待っているかが分かる。`ExecSbx` は cobra の context を使っている。timeout は command の側で付けない限り無い |
| pprof・trace | 無い | 優先度は低い。所要時間の大半は `sbx` の待ち時間と見込まれ、slog に所要時間を出せば足りる。入れるなら、`main` は error のとき `os.Exit(1)` するので、止める処理を `os.Exit` の前に置く |
| macOS | debugserver は `/Library/Developer/CommandLineTools/.../debugserver` にある（ローカルで確認） | 他の開発者の Mac では、`xcode-select --install` と、必要なら `DevToolsSecurity -enable` が要る |

## 出典

Go:

- `go help build` / `go help run` / `go help testflag`、`go tool compile -help` / `go tool link -help`、`go doc runtime`・`runtime/debug`・`log/slog`・`testing`・`os`・`os/exec`・`os/signal`・`runtime/pprof`・`runtime/trace`（go1.27.1 で実行）
- [cmd/go](https://pkg.go.dev/cmd/go)、[cmd/compile](https://pkg.go.dev/cmd/compile)、[cmd/link](https://pkg.go.dev/cmd/link)
- Go の source（go1.27.1）:
  - [cmd/go/internal/run/run.go#L151](https://github.com/golang/go/blob/go1.27.1/src/cmd/go/internal/run/run.go#L151)
  - [cmd/go/internal/work/gc.go#L605](https://github.com/golang/go/blob/go1.27.1/src/cmd/go/internal/work/gc.go#L605)
  - [cmd/go/internal/work/build.go#L472](https://github.com/golang/go/blob/go1.27.1/src/cmd/go/internal/work/build.go#L472)
  - [cmd/go/internal/load/pkg.go#L2547](https://github.com/golang/go/blob/go1.27.1/src/cmd/go/internal/load/pkg.go#L2547)
  - [os/exec/exec_test.go](https://github.com/golang/go/blob/go1.27.1/src/os/exec/exec_test.go)
  - 比較用に [go1.18 の os/exec/exec_test.go](https://github.com/golang/go/blob/go1.18/src/os/exec/exec_test.go#L681)
- [Data Race Detector](https://go.dev/doc/articles/race_detector)（golang/website の `_content/doc/articles/race_detector.html` で照合）
- [Go, Backwards Compatibility, and GODEBUG](https://go.dev/doc/godebug)（`$GOROOT/doc/godebug.md` で照合）
- [Go 1.24 release notes](https://go.dev/doc/go1.24)（golang/website の `_content/doc/go1.24.md` で照合）
- pkg.go.dev:
  - [runtime](https://pkg.go.dev/runtime#hdr-Environment_Variables)
  - [runtime/debug](https://pkg.go.dev/runtime/debug)
  - [log/slog](https://pkg.go.dev/log/slog)
  - [testing](https://pkg.go.dev/testing)
  - [os](https://pkg.go.dev/os)
  - [os/exec](https://pkg.go.dev/os/exec)
  - [os/signal](https://pkg.go.dev/os/signal)
  - [runtime/pprof](https://pkg.go.dev/runtime/pprof)
  - [runtime/trace](https://pkg.go.dev/runtime/trace)

Delve v1.27.2（文書と source は module cache の `github.com/go-delve/delve@v1.27.2` で読み、`dlv help …` は実行して確かめた）:

- [Documentation/usage/](https://github.com/go-delve/delve/tree/v1.27.2/Documentation/usage)
- [Documentation/faq.md](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/faq.md)
- [Documentation/installation/README.md](https://github.com/go-delve/delve/blob/v1.27.2/Documentation/installation/README.md)
- [cmd/dlv/cmds/commands.go](https://github.com/go-delve/delve/blob/v1.27.2/cmd/dlv/cmds/commands.go)
- [pkg/proc/gdbserial/gdbserver.go](https://github.com/go-delve/delve/blob/v1.27.2/pkg/proc/gdbserial/gdbserver.go)
- [service/debugger/debugger.go](https://github.com/go-delve/delve/blob/v1.27.2/service/debugger/debugger.go)
- [pkg/gobuild/](https://github.com/go-delve/delve/tree/v1.27.2/pkg/gobuild)
- [pkg/proc/macutil/rosetta_darwin.go](https://github.com/go-delve/delve/blob/v1.27.2/pkg/proc/macutil/rosetta_darwin.go)

エディタ:

- [vscode-go docs/debugging.md（commit 7ce26ce）](https://github.com/golang/vscode-go/blob/7ce26ceacc5b29185ae15025705c654a2969aadc/docs/debugging.md)
- [GoLand: Debugging code](https://www.jetbrains.com/help/go/debugging-code.html)（未検証）

testscript:

- [go-internal README](https://github.com/rogpeppe/go-internal#readme)
- [testscript v1.16.0 doc.go](https://github.com/rogpeppe/go-internal/blob/v1.16.0/testscript/doc.go)
- [exe.go](https://github.com/rogpeppe/go-internal/blob/v1.16.0/testscript/exe.go)
- [pkg.go.dev](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript)（v1.16.0、2026-07-01 公開）
