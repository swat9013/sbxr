#!/usr/bin/env bash
set -euo pipefail

# origin/main に tag を打てるかを判定する材料と、Conventional Commits から出した版番号の候補を key=value で出す

git fetch -q origin --tags
sha=$(git rev-parse origin/main)
ci=$(gh run list -w ci.yml -b main --commit "$sha" -L 1 --json status,conclusion \
  -q '.[0] // {} | "\(.status // "none")/\(.conclusion // "")"')
prev=$(git describe --tags --abbrev=0 origin/main)
prev_type=$(git cat-file -t "$prev")

subjects=$(git log --no-merges --format='%s' "$prev..origin/main")
count() { grep -cE "$1" <<<"$subjects" || true; }
total=$(count '.')
feat=$(count '^feat(\([^)]*\))?!?:')
fix=$(count '^(fix|perf)(\([^)]*\))?!?:')
breaking=$(count '^[a-z]+(\([^)]*\))?!:')
footer=$(git log --format='%B' "$prev..origin/main" | grep -cE '^BREAKING[ -]CHANGE:' || true)
breaking=$((breaking + footer))

IFS=. read -r major minor patch <<<"${prev#v}"
next=ask
if [ "$breaking" -eq 0 ] && [ "$feat" -gt 0 ]; then
  next="v$major.$((minor + 1)).0"
elif [ "$breaking" -eq 0 ] && [ "$fix" -gt 0 ]; then
  next="v$major.$minor.$((patch + 1))"
fi

cat <<OUT
sha=$sha
ci=$ci
prev=$prev
prev_type=$prev_type
commits=$total
feat=$feat
fix=$fix
breaking=$breaking
next=$next
OUT
