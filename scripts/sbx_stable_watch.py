"""sbx の最新の stable が実測済みの上限より新しいかを判定し、立てる issue の題名と本文を出す。

workflow .github/workflows/sbx-stable-watch.yml が週 1 回呼ぶ。判定だけを持ち、GitHub には触れない。
立てる issue があれば題名を標準出力へ、本文を --body-out の file へ書く。無ければ何も出さずに終わる。
"""

import argparse
import pathlib
import re
import sys

# 版の形は internal/runtime/sbx_version.go の parseSbxVersion と同じ: 各欄は数字だけで、先頭の 0 を持たない
VERSION_PATTERN = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")
MEASURED_PATTERN = re.compile(
    r"MeasuredSbxVersion\s*=\s*SbxVersion\{Major:\s*([0-9]+),\s*Minor:\s*([0-9]+),\s*Patch:\s*([0-9]+)\}"
)


def parse_version(tag):
    match = VERSION_PATTERN.fullmatch(tag)
    if match is None:
        raise ValueError(f"版 {tag!r} が v<major>.<minor>.<patch> の形でない")
    return tuple(int(part) for part in match.groups())


def measured_version(go_source):
    match = MEASURED_PATTERN.search(go_source)
    if match is None:
        raise ValueError("MeasuredSbxVersion = SbxVersion{Major: ..., Minor: ..., Patch: ...} の行が見つからない")
    return tuple(int(part) for part in match.groups())


def issue_title(tag):
    # 題名は、同じ版の issue が既にあるかの照合にも使う。文面を変えると、立て済みの版の issue をもう一度立てる
    return f"sbx {tag} を実 sbx で確かめる"


def new_issue_title(latest_tag, measured, existing_titles):
    """立てる issue の題名を返す。立てなくてよければ None を返す。"""
    if parse_version(latest_tag) <= measured:
        return None
    title = issue_title(latest_tag)
    if title in existing_titles:
        return None
    return title


def issue_body(latest_tag, measured):
    measured_tag = "v{}.{}.{}".format(*measured)
    return f"""## 解く課題

sbx の stable に {latest_tag} が出た（https://github.com/docker/sbx-releases/releases/tag/{latest_tag}）。実測済みの上限（`internal/runtime/sbx_version.go` の `MeasuredSbxVersion`、{measured_tag}）より新しく、sbxr がこの版で動くかを確かめていない。

この issue は workflow `.github/workflows/sbx-stable-watch.yml` が立てた。

## 受け入れ条件

- sbx {latest_tag} の実 sbx（macOS）で `docs/real-sbx-walkthrough-macos.md` の 1 周を通し、手順と結果を PR 本文に残している
- `MeasuredSbxVersion` が {latest_tag} に上がっている。1 周が通らなければ、直してから上げる

## 設計

- 追従の規約: CONTRIBUTING.md「sbx の新しい版への追従」
- 上限の意味: docs/design/sbxr/decision/0019-doctor-checks-sbx-version-range.md

## 範囲

`internal/runtime/sbx_version.go` と、1 周で壊れた箇所を直す file とその test。範囲外で見つけた欠陥・指摘は `docs/agents/issue-tracker.md`「follow-up issue の起票」に従って扱う。
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--latest-tag", required=True, help="docker/sbx-releases の最新の stable の tag")
    parser.add_argument("--version-file", required=True, type=pathlib.Path, help="internal/runtime/sbx_version.go")
    parser.add_argument(
        "--existing-titles", required=True, type=pathlib.Path, help="既存の issue (open と closed) の題名を 1 行 1 件で並べた file"
    )
    parser.add_argument("--body-out", required=True, type=pathlib.Path, help="立てる issue の本文を書く file")
    args = parser.parse_args()

    existing_titles = args.existing_titles.read_text(encoding="utf-8").splitlines()
    try:
        measured = measured_version(args.version_file.read_text(encoding="utf-8"))
        title = new_issue_title(args.latest_tag, measured, existing_titles)
    except ValueError as err:
        # 読めない版を「新しい版は無い」と読み替えると、監視が黙って止まる。run を落とし、理由を run の要約に出す
        sys.exit(f"::error::{err}")
    if title is None:
        return
    args.body_out.write_text(issue_body(args.latest_tag, measured), encoding="utf-8")
    print(title)


if __name__ == "__main__":
    main()
