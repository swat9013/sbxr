# sbxr

repo を宣言 1 枚で AI coding agent 用の隔離環境（[Docker Sandboxes](https://docs.docker.com/ai/sandboxes/) の sandbox VM）にする CLI。

> Status: 設計段階（v0.1 未リリース）。語彙は [CONTEXT.md](./CONTEXT.md)、設計判断は [docs/adr/](./docs/adr/) を参照。

## できること（v0.1 の予定）

- `sbxr plan <repo>` — 何が作られるかを表示する
- `sbxr create <repo>` — repo 宣言（`sbxr.yaml`）を user 設定と merge し、確認のうえ sandbox VM を作る。egress 許可・secret 配線・agent runtime profile・init / boot を適用する
- `sbxr fetch <repo>` — sandbox VM 内の変更を host へ取り込む
- `sbxr stop <repo>` / `sbxr destroy <repo>` — 停止 / 撤去（撤去前に未回収の変更を検査する）
- `sbxr policy sync [--check]` — user 設定の egress 宣言を global rule へ収束させる
- `sbxr secret setup github` / `sbxr secret setup custom --host <host>` — token の能力を確認して secret ファイルへ格納する

`<repo>` はローカル path か git URL。

## 必要なもの

- [`sbx`](https://docs.docker.com/ai/sandboxes/)（Docker Sandboxes CLI）
- macOS、または sbx が公式に対応する Linux（Ubuntu 24.04 以上 + KVM。Linux の実機動作は未確認）

## 設定ファイル

| ファイル | 役割 |
|---|---|
| `<repo>/sbxr.yaml` | repo 宣言（untrusted として扱う） |
| `~/.config/sbxr/config.yaml` | user 設定 |
| `~/.config/sbxr/secrets.env` | secret の値（mode 0600 必須） |

## インストール（v0.1 以降）

```sh
brew install swat9013/tap/sbxr
# または
go install github.com/swat9013/sbxr/cmd/sbxr@latest
```

## License

[MIT](./LICENSE)
