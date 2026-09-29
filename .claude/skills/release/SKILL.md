---
name: release
description: origin/main の HEAD に `v*` tag を打って push し、sbxr の新しい版を GitHub Release と Homebrew cask に出す。Use when「リリースして」「版を上げて出す」.
---

# release

リリースの規約（tag を打つ条件・版番号の決め方・tag の扱い）の正本は `CONTRIBUTING.md` の「リリース」節。最初に Read し、この skill はその規約をどう実行するかだけを扱う。ファイルは編集しない。

## 手順

### 1. 材料を集める

repo root で `./.claude/skills/release/scripts/preflight.sh` を実行する。出力は次の key=value。

| key | 中身 |
|---|---|
| `sha` | fetch した後の `origin/main` の SHA |
| `ci` | その SHA の CI run の `status/conclusion`。run が無ければ `none/` |
| `prev`・`prev_type` | 前の tag と、その object の種類（`commit` なら lightweight tag） |
| `commits`・`feat`・`fix`・`breaking` | 前の tag からの commit の数（`fix` には `perf` も含む） |
| `next` | 規約から出した版番号の候補。規約が user の判断に回す場合は `ask` |

`ci` の値で次を決める。

- `completed/success` なら 2 へ進む
- `in_progress/` か `queued/` なら、`gh run list -w ci.yml --commit <sha>` で run の id を引き、`gh run watch <id> --exit-status` で待ってから script を実行し直す
- それ以外なら、tag を打たずに状況を伝えて止める

完了条件: `ci` が `completed/success`。

### 2. user の承認を得て tag を push する

`next`・根拠の数（feat・fix・breaking）・`sha` を示す。`next` が `ask` なら版番号を user に決めてもらう。どちらの場合も、user の承認を得てから push する。push の時点で permission の確認も出る（`.claude/settings.json` の `ask`）。

```sh
git tag vX.Y.Z <sha>          # prev_type が tag なら -a を付けて注釈付きにする
git push origin vX.Y.Z
```

### 3. release workflow の完了を待つ

```sh
gh run list -w release.yml -b vX.Y.Z --json databaseId,status
gh run watch <databaseId> --exit-status
```

push した直後は、run がまだ一覧に出ないことがある。0 件なら、少し待ってから list を取り直す。

完了条件: `ci / ci-ok` と `release` の job が両方 success。失敗したら、tag は消さずに（`CONTRIBUTING.md` の規約）、`gh run view <id> --log-failed` の該当箇所を添えて user に返す。

### 4. 出たものを確かめる

```sh
gh release view vX.Y.Z --json url,assets
brew list --cask swat9013/tap/sbxr   # この host に cask で入っているか
brew update && brew upgrade --cask swat9013/tap/sbxr && sbxr --version
```

cask で入っていない host では upgrade を飛ばし、`brew info --cask swat9013/tap/sbxr` の版表示で cask が出たことを確かめる。

完了条件: 次の 2 つがそろう。

- Release に darwin 版と linux 版（arm64・amd64）の asset と `checksums.txt` がある
- `sbxr --version` の出力（cask で入っていない host では `brew info` の版表示）に `vX.Y.Z` が含まれる

Release notes は goreleaser が生成したものをそのまま使う。

終わったら、版番号とその根拠、Release の URL、`sbxr --version` の結果を、結論から短く返す。
