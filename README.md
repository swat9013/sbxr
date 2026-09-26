# sbxr

repo を宣言 1 枚で AI coding agent 用の隔離環境（[Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) の sandbox VM）にする CLI。

> Status: 設計段階（v0.1 未リリース）。語彙は [CONTEXT.md](./CONTEXT.md)、設計判断は [docs/adr/](./docs/adr/) を参照。

## できること（v0.1 の予定）

- `sbxr plan <repo>` — 何が作られるかを表示する
- `sbxr create <repo>` — repo 宣言（`sbxr.yaml`）を user 設定と merge し、確認のうえ sandbox VM を作る。egress 許可・secret 配線・agent runtime profile・init / boot を適用する
- `sbxr stop <repo>` / `sbxr destroy <repo>` — 停止 / 撤去（撤去すると VM 内の commit と変更は失われるので、確認を求める。稼働中の VM は使用中かを確かめられないので、止めてから撤去するか `--force` を付ける）
- `sbxr policy sync [--check]` — user 設定の egress 宣言を global rule へ収束させる
- `sbxr secret setup github` / `sbxr secret setup custom --host <host>` — token の能力を確認して secret ファイルへ格納する

`<repo>` はローカル path か git URL。

sandbox VM 内の commit は、VM の稼働中に host 側 repo で `git fetch sandbox-<name>` を実行して取り込むか、VM 内から origin へ push して取り出す。

## 置き場

- 状態ディレクトリ: `${XDG_STATE_HOME:-~/.local/state}/sbxr/sandboxes/<name>/`（env 定義・作成時の宣言・埋め込みの kit。destroy が消す）
- git URL の cache clone: `${XDG_CACHE_HOME:-~/.cache}/sbxr/repos/<name>/`（destroy が消す）

## init と boot

- `init` は create のときに 1 回、VM 内の repo root で順に走る。失敗すると create は VM を残して止まる
- `boot` は create のときに init の後で 1 回走り、その後は sandbox VM の起動ごとに走る。走るのは作成時に確定した内容で、起動のたびに宣言を読み直しはしない
- 2 回目以降の起動での boot の出力と失敗（`boot[N] fail`）は host からは見えない。VM 内の `/var/log/sbx-kit-startup.log` に残る（`sbx exec <name> -- cat /var/log/sbx-kit-startup.log`）

## herdr 連携（opt-in）

user 設定で有効にすると、VM に [herdr](https://github.com/herdrdev/herdr) を入れ、host の herdr に `<name>.sbx` を machine として登録する。repo 宣言には書けない。

```yaml
# ~/.config/sbxr/config.yaml
herdr:
  enabled: true
  version: v0.9.0   # 省略すると同梱の動作確認済みの版
```

- VM 内では起動ごとに、宣言の版と違うときだけ GitHub release から herdr を入れ、`herdr server` を起動し、`herdr integration install claude` を実行する
- create の後に登録し、destroy の前に解除する。stop の前には machine を無効にする（有効なままだと herdr が繋ぎ直して VM が起動する）。起動し直したら、stop が表示した `herdr machine enable <id>` で有効に戻す
- 有効なのに host に herdr が無ければ、create / stop / destroy は確認や VM の操作の前に止まる。登録に失敗したら VM を残して止まり、登録し直す手順を表示する（同じ `<name>.sbx` の登録が残っているときも、それには触れずに止まる）
- VM 内の herdr の失敗は `/var/log/sbx-kit-startup.log` に `sbxr-herdr: fail` の行で残る。create のときは create が失敗として止まる

## 必要なもの

- [`sbx`](https://docs.docker.com/ai/sandboxes/)（Docker Sandboxes CLI）
- herdr 連携を有効にするときだけ、host の `herdr`
- macOS、または sbx が公式に対応する Linux（Ubuntu 24.04 以上 + KVM。Linux の実機動作は未確認）

## 設定ファイル

| ファイル | 役割 |
|---|---|
| `<repo>/sbxr.yaml` | repo 宣言（untrusted として扱う） |
| `~/.config/sbxr/config.yaml` | user 設定 |
| `~/.config/sbxr/secrets.env` | secret の値（mode 0600 必須） |

## secret

VM には placeholder だけが入り、実値は host 側の proxy が通信時に差し込む。secret は「定義」と「要求」に分けて書く。

```yaml
# ~/.config/sbxr/config.yaml
version: 1
secrets: [github]          # 要求 (repo 宣言にも書ける)。user 設定の要求は全 sandbox VM に効く
secret_defs:               # 定義 (user 設定だけが書ける。github は同梱)
  gitlab:
    key: GITLAB_TOKEN      # secret ファイルのキー
    hosts: [gitlab.example.com]  # 注入先 host。すべてが egress で許可されているときだけ配線する
    env: GITLAB_TOKEN      # placeholder を入れる VM の環境変数名
    vars:                  # 秘密でない付随値
      GITLAB_HOST: gitlab.example.com
```

値は `sbxr secret setup github` か `sbxr secret setup custom --host <host>` で secret ファイルへ書く。`setup github` は、指定した private repo（commit が 1 つ以上あるもの）で token が Contents を読めて、Secrets・Actions・Administration が拒否されることを GitHub API で確かめてから書く。`.github/workflows` を書き換える Workflows 権限は読み取りの API で確かめられないので、token を作るときに付けないこと。`setup custom` は `--host` を注入先に持つ secret 定義の key へ書く。

repo の削除と force push は token の権限では防げない。守りたい branch には branch protection（または ruleset）を設定する。

## インストール（v0.1 以降）

```sh
brew install swat9013/tap/sbxr
# または
go install github.com/swat9013/sbxr/cmd/sbxr@latest
```

## License

[MIT](./LICENSE)
