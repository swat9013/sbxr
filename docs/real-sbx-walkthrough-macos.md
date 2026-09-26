# 実 sbx での 1 周（macOS）

fixture の repo から sandbox VM を作り、create → boot → stop → start → destroy を 1 周する。
見るのは次の 2 点。

- 宣言の意味が実 sbx の上でも保たれること
- boot が作成時だけでなく、止めた後の起動でも再実行されること（ADR 0006）

同じ意味の自動検査は `cmd/sbxr/acceptance_test.go`（sbx stub 上）にある。この手順は、stub では再現できない実 sbx の振る舞いを確かめるためのもの。

## 触ってよい範囲

- 作るのは fixture の sandbox VM 1 つだけにする。途中で失敗しても、その VM は destroy して片付ける
- global rule を変えない。`sbxr policy sync` を実行せず、user 設定に egress を書かない
- 状態ディレクトリと cache clone は、`XDG_STATE_HOME`・`XDG_CACHE_HOME` で一時ディレクトリへ向ける
- sbxr の user 設定と secret ファイルは `~/.config/sbxr/` に固定されている（`XDG_CONFIG_HOME` を読まない。ADR 0002 / 0004）。この手順はそこを読むだけで、書かない。そこに `config.yaml` があれば、その `herdr`・`secrets`・`init`・`boot` が fixture の VM にも効く。その場合は 1 周の間だけ別の場所へ退避する

`HOME` は変えない。sbx は認証と daemon の状態を `$HOME/Library/Application Support/com.docker.sandboxes/` に持つ。そのため、`HOME` を一時ディレクトリへ向けると、sbx は `error: Not authenticated to Docker` で止まる（2026-09-26、sbx v0.45.1 で確認）。

## 必要なもの

- macOS と [`sbx`](https://docs.docker.com/ai/sandboxes/)（`sbx login` 済み）
- Go（この repo の checkout から sbxr を build する）
- git

## 0. 準備

この repo の root で実行する。

```sh
sbx version                       # 版を記録する
sbx ls                            # 認証が通ること、fixture と同じ名前の VM が無いことを確かめる
ls ~/.config/sbxr/                # 無い、または退避した後であること

export WORK="$(mktemp -d)"
export XDG_STATE_HOME="$WORK/state" XDG_CACHE_HOME="$WORK/cache"
go build -o "$WORK/sbxr" ./cmd/sbxr
```

## 1. fixture の repo を作る

VM の名前は repo のディレクトリ名になる（ここでは `sbxr-fixture`）。

```sh
FIXTURE="$WORK/sbxr-fixture"
mkdir "$FIXTURE"
cat > "$FIXTURE/sbxr.yaml" <<'YAML'
version: 1
git:
  name: sbxr-fixture
  email: sbxr-fixture@example.invalid
init:
  - echo init >> .sbxr-init-marker
boot:
  - echo "boot $(date +%s)" >> .sbxr-boot-marker
YAML
git -C "$FIXTURE" init -q
git -C "$FIXTURE" add sbxr.yaml
git -C "$FIXTURE" -c user.name=sbxr-fixture -c user.email=sbxr-fixture@example.invalid commit -q -m fixture
```

marker は VM 内の repo root（VM 内の path は host と同じ `$FIXTURE`）に追記される。host の `$FIXTURE` には現れない（env 定義の `workspace.clone: true` は repo を VM 内へ clone する。2026-09-26、sbx v0.45.1 で確認）。そのため marker は `sbx exec -w` で VM 内を見る。

## 2. create（init と 1 回目の boot）

```sh
"$WORK/sbxr" plan "$FIXTURE"
"$WORK/sbxr" create "$FIXTURE"     # 確認関門で要約を読み、y で承認する
```

確認への答えは端末から 1 行読む。複数行をまとめて貼り付けると、続きの行が答えとして読まれて `中止した` で止まることがある。コマンドは 1 行ずつ実行する。

期待する結果:

- 要約に git identity・init・boot が出てから、確認を求められる
- 出力に `init[1]: echo init >> .sbxr-init-marker` と `boot: 1 件を VM の ~/.config/sbxr/boot.sh に書いた` が出て、最後に `sandbox VM sbxr-fixture を作った` で終わる
- `ls "$XDG_STATE_HOME/sbxr/sandboxes/sbxr-fixture/"` に `sbxenv.yaml`・`source`・`declaration.yaml`・`kits/sbxr-boot/` がある

```sh
sbx exec -w "$FIXTURE" sbxr-fixture -- wc -l .sbxr-init-marker .sbxr-boot-marker   # init 1 行、boot 1 行
```

## 3. stop

```sh
"$WORK/sbxr" stop "$FIXTURE"
sbx ls                            # sbxr-fixture が stopped
```

## 4. start（sbx の起動コマンドを直接使う）

sbxr に start は無い。`sbx exec` は、止まった sandbox を先に起動してからコマンドを実行する（`sbx exec --help`）。この起動でも kit の startup が走る（2026-09-26、sbx v0.45.1 で確認）。

```sh
sbx exec sbxr-fixture -- true     # Sandbox sbxr-fixture started successfully
sbx ls                            # sbxr-fixture が running
```

## 5. boot の再実行を確かめる

kit の startup は起動の後に走るので、行が増えるまで数秒待つことがある。

```sh
sbx exec -w "$FIXTURE" sbxr-fixture -- wc -l .sbxr-init-marker .sbxr-boot-marker   # init 1 行のまま、boot 2 行
sbx exec sbxr-fixture -- cat /var/log/sbx-kit-startup.log                           # 再起動の回に boot[1]: start があり、boot[1] fail が無い
```

判定の主軸は marker の行数にする。init は増えず、boot だけが 1 行増えていれば、boot が起動ごとに再生されている。

`/var/log/sbx-kit-startup.log` の create 時の回には `boot[1]: start` が無いのが正しい。create 時の kit の startup は boot.sh を書く前に走って何もせず、1 回目の boot は sbxr が直接実行して出力を host に出すため（ADR 0006）。

## 6. destroy

```sh
"$WORK/sbxr" stop "$FIXTURE"
"$WORK/sbxr" destroy "$FIXTURE"   # VM 内の変更が失われる旨を読み、y で承認する
```

## 7. 片付けを確かめる

```sh
sbx ls                                        # sbxr-fixture が無い
ls "$XDG_STATE_HOME/sbxr/sandboxes/"          # sbxr-fixture が無い
rm -rf "$WORK"
```

`~/.config/sbxr/` を退避していたら戻す。

## 結果の残し方

PR 本文に、step ごとの結果を checklist で残す。

- sbx の版
- 実行したコマンドと、期待と違った出力
- step 5 の marker の行数（init / boot）と、`/var/log/sbx-kit-startup.log` の該当行
- step 4 で使った start のコマンド
- 後片付けの状態（`sbx ls` の出力）
