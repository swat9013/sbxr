---
name: release
description: origin/main の HEAD に `v*` tag を打って push し、sbxr の新しい版を GitHub Release と Homebrew cask に出す。Use when「リリースして」「版を上げて出す」.
---

# release

`v*` tag を push すると、release workflow（`.github/workflows/release.yml`）が CI を通したうえで goreleaser を走らせ、GitHub Release と `swat9013/homebrew-tap` の cask を出す。この skill がやるのは、tag を打つ前の確認から、tag の push、結果の確認まで。ファイルは編集しない。基準は常に `origin/main` の SHA なので、どの checkout から動かしてもよい。

## 手順

### 1. origin/main が出せる状態か確かめる

```sh
git fetch origin --tags
git rev-parse origin/main
gh run list -w ci.yml -b main -L 5 --json headSha,status,conclusion
```

`headSha` が `origin/main` の SHA と一致する run を見る。連続して merge すると待機中の run が `cancelled` になるので、他の run の結果は判断に使わない。

- success → 2 へ
- 実行中 → `gh run watch <id> --exit-status` で待つ
- 失敗 → tag を打たずに状況を伝えて止める。落ちると分かっている commit には tag を打たない

完了条件: `origin/main` の SHA の CI が success。

### 2. 版番号を決める

```sh
git describe --tags --abbrev=0 origin/main   # 前の tag
git log --no-merges --format='%s' <前の tag>..origin/main
git log --no-merges --format='%s' <前の tag>..origin/main | grep -E '^[a-z]+(\([^)]*\))?!:'
git log --format='%B' <前の tag>..origin/main | grep -E '^BREAKING[ -]CHANGE:'
```

- breaking の印（subject の `!` か footer の `BREAKING CHANGE:`）がある → 0.x の間の major の扱いがまだ決まっていないので、版番号は user に決めてもらう
- `feat` がある → minor を上げる
- `fix`・`perf` がある → patch を上げる
- docs・refactor・test・chore・ci だけ、または差分が 0 件 → 利用者から見える変更が無いので、出すかどうかを user に決めてもらう

完了条件: 版番号と、根拠にした feat・fix・breaking の有無が言える。

### 3. user の承認を得て tag を push する

版番号・根拠・tag を打つ SHA を示し、user の承認を得てから push する。push した tag は公開され、Release と cask が出る。

tag は前の tag と同じ形にする。`git cat-file -t <前の tag>` が `commit` なら lightweight tag。

```sh
git tag vX.Y.Z <1 で確かめた origin/main の SHA>
git push origin vX.Y.Z
```

push した後は、4 が失敗しても、tag を消したり打ち直したりする前に user に確認する。goreleaser が Release や cask を途中まで出していることがあるため。

### 4. release workflow の完了を待つ

```sh
gh run list -w release.yml -b vX.Y.Z --json databaseId,status
gh run watch <databaseId> --exit-status
```

push した直後は、run がまだ一覧に出ないことがある。0 件なら、少し待ってから list を取り直す。

完了条件: `ci / ci-ok` と `release` の job が両方 success。失敗したら、`gh run view <id> --log-failed` の該当箇所を添えて user に返す。

### 5. 出たものを確かめる

```sh
gh release view vX.Y.Z --json url,assets
brew list --cask swat9013/tap/sbxr   # この host に cask で入っているか
brew update && brew upgrade --cask swat9013/tap/sbxr && sbxr --version
```

cask で入っていない host では upgrade を飛ばす。cask が出たことは `brew info --cask swat9013/tap/sbxr` の版表示で確かめる。

完了条件: 次の 2 つがそろう。

- Release に darwin 版と linux 版（arm64・amd64）の asset と `checksums.txt` がある
- `sbxr --version` の出力（cask で入っていない host では `brew info` の版表示）に `vX.Y.Z` が含まれる

Release notes は goreleaser が生成したものをそのまま使う。

終わったら、版番号とその根拠、Release の URL、`sbxr --version` の結果を、結論から短く返す。
