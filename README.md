# sbxr

repo を宣言 1 枚で AI coding agent 用の隔離環境（[Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) の sandbox VM）にする CLI。

> Status: 設計段階（v0.1 未リリース）。語彙は [CONTEXT.md](./CONTEXT.md)、設計判断は [docs/adr/](./docs/adr/) を参照。

## できること（v0.1 の予定）

- `sbxr plan <repo>` — 何が作られるかを表示する
- `sbxr create <repo>` — repo 宣言（`sbxr.yaml`）を user 設定と merge し、確認のうえ sandbox VM を作る。egress 許可・secret 配線・agent runtime profile・init / boot を適用する
- `sbxr stop <repo>` / `sbxr destroy <repo>` — 停止 / 撤去（撤去すると VM 内の commit と変更は失われるので、確認を求める）
- `sbxr policy sync [--check]` — user 設定の egress 宣言を global rule へ収束させる
- `sbxr secret setup github` / `sbxr secret setup custom --host <host>` — token の能力を確認して secret ファイルへ格納する

`<repo>` はローカル path か git URL。

sandbox VM 内の commit は、VM の稼働中に host 側 repo で `git fetch sandbox-<name>` を実行して取り込むか、VM 内から origin へ push して取り出す。

## 必要なもの

- [`sbx`](https://docs.docker.com/ai/sandboxes/)（Docker Sandboxes CLI）
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

値は `sbxr secret setup github` か `sbxr secret setup custom --host <host>` で secret ファイルへ書く。`setup github` は、指定した private repo で token が Contents を読めて、Secrets・Workflows・Administration を持たないことを GitHub API で確かめてから書く。

repo の削除と force push は token の権限では防げない。守りたい branch には branch protection（または ruleset）を設定する。

## インストール（v0.1 以降）

```sh
brew install swat9013/tap/sbxr
# または
go install github.com/swat9013/sbxr/cmd/sbxr@latest
```

## License

[MIT](./LICENSE)
