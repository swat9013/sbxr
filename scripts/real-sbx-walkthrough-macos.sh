#!/usr/bin/env bash
set -euo pipefail

# docs/real-sbx-walkthrough-macos.md の 1 周（fixture の repo → create → stop → start → boot の再実行 → destroy → 片付け）を
# 実 sbx の上で順に実行し、期待する結果を確かめる。--yes を付けると create と destroy の確認を省く。

case "${1:-}" in
  "") yes= ;;
  --yes) yes=1 ;;
  *) echo "usage: $0 [--yes]" >&2; exit 2 ;;
esac

VM=sbxr-fixture
cd "$(dirname "$0")/.."

ok() { echo "ok: $*"; }
ng() { echo "NG: $*" >&2; exit 1; }
step() { printf '\n== %s\n' "$*"; }
vm_row() { sbx ls | awk -v vm="$VM" '$1 == vm'; }
# VM 内の repo root（host と同じ path）にある marker の行数。無ければ 0
marker_lines() { sbx exec -w "$FIXTURE" "$VM" -- sh -c "cat $1 2>/dev/null | wc -l" | tail -n 1 | tr -dc 0-9; }

step "0. 準備"
sbx version
[ -z "$(vm_row)" ] || ng "$VM という sandbox VM が既にある"
[ ! -e ~/.config/sbxr/config.yaml ] ||
  ng "$HOME/.config/sbxr/config.yaml を 1 周の間だけ退避する（herdr・secrets・init・boot が fixture の VM にも効くため）"

WORK="$(mktemp -d)"
export XDG_STATE_HOME="$WORK/state" XDG_CACHE_HOME="$WORK/cache"
FIXTURE="$WORK/$VM"
SBXR="$WORK/sbxr"
created=
on_exit() {
  local status=$?
  [ "$status" -eq 0 ] && return
  echo "失敗した（exit $status）。作業ディレクトリ $WORK は残した" >&2
  [ -n "$created" ] && cat >&2 <<EOF
sandbox VM が残っている。調べ終えたら片付ける:
  export XDG_STATE_HOME="$XDG_STATE_HOME" XDG_CACHE_HOME="$XDG_CACHE_HOME"
  "$SBXR" stop "$FIXTURE"; "$SBXR" destroy "$FIXTURE"; rm -rf "$WORK"
EOF
}
trap on_exit EXIT

go build -o "$SBXR" ./cmd/sbxr
"$SBXR" --version

step "1. fixture の repo を作る"
mkdir "$FIXTURE"
cat >"$FIXTURE/sbxr.yaml" <<'YAML'
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
ok "$FIXTURE"

step "2. create（init と 1 回目の boot）"
"$SBXR" plan "$FIXTURE"
created=1
"$SBXR" create ${yes:+"--yes"} "$FIXTURE"
for f in sbxenv.yaml source declaration.yaml kits/sbxr-boot; do
  [ -e "$XDG_STATE_HOME/sbxr/sandboxes/$VM/$f" ] || ng "状態ディレクトリに $f が無い"
done
ok "状態ディレクトリに sbxenv.yaml・source・declaration.yaml・kits/sbxr-boot がある"
[ "$(marker_lines .sbxr-init-marker)/$(marker_lines .sbxr-boot-marker)" = 1/1 ] || ng "create の後の marker が init 1 行・boot 1 行でない"
ok "create の後の marker は init 1 行・boot 1 行"

step "3. stop"
"$SBXR" stop "$FIXTURE"
vm_row | grep -qw stopped || ng "stop の後に $VM が stopped でない: $(vm_row)"
ok "$VM は stopped"

step "4. start（sbx exec が止まった VM を起動する）"
sbx exec "$VM" -- true
vm_row | grep -qw running || ng "start の後に $VM が running でない: $(vm_row)"
ok "$VM は running"

step "5. boot の再実行を確かめる"
for _ in $(seq 30); do # kit の startup は起動の後に走るので、boot の行が増えるまで待つ
  [ "$(marker_lines .sbxr-boot-marker)" -ge 2 ] && break
  sleep 1
done
[ "$(marker_lines .sbxr-init-marker)/$(marker_lines .sbxr-boot-marker)" = 1/2 ] || ng "再起動の後の marker が init 1 行・boot 2 行でない"
ok "再起動の後の marker は init 1 行・boot 2 行"
log=$(sbx exec "$VM" -- cat /var/log/sbx-kit-startup.log)
grep -qF 'boot[1] fail' <<<"$log" && ng "startup log に boot[1] fail がある"
grep -qF 'boot[1]: start' <<<"$log" || ng "startup log に boot[1]: start が無い"
ok "startup log に boot[1]: start があり、boot[1] fail が無い"

step "6. destroy"
"$SBXR" stop "$FIXTURE"
"$SBXR" destroy ${yes:+"--yes"} "$FIXTURE"

step "7. 片付けを確かめる"
[ -z "$(vm_row)" ] || ng "destroy の後も $VM が sbx ls にある"
[ ! -e "$XDG_STATE_HOME/sbxr/sandboxes/$VM" ] || ng "destroy の後も状態ディレクトリが残っている"
rm -rf "$WORK"
ok "VM・状態ディレクトリ・作業ディレクトリを片付けた"
