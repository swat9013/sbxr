package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/swat9013/sbxr/internal/egress"
	"github.com/swat9013/sbxr/internal/runtime"
)

// probeScript は $1 へ GET を 1 回送り (redirect は追わない)、1 行目に「curl の exit code と状態コード」を、
// 続けて body の先頭 (curl が失敗したら curl の error) を出す。判定は sbxr が出力から行うので、script は 0 で終える。
const probeScript = `out=$(mktemp) && err=$(mktemp) || exit 1
code=$(curl -sS --max-time 20 -o "$out" -w '%{http_code}' "$1" 2>"$err"); status=$?
echo "$status $code"
if [ "$status" -eq 0 ]; then head -c 1024 "$out"; else head -c 1024 "$err"; fi
rm -f "$out" "$err"`

// proxyDenial は proxy の拒否応答の body に入る文言 (sbx v0.45.1 の実測。decision/0007)。
const proxyDenial = "Blocked by network policy"

// probeResult は VM 内から 1 つの宛先へ送った結果。
type probeResult int

const (
	// probeReached は proxy の拒否応答でない HTTP 応答が返った (状態コードを問わない)。
	probeReached probeResult = iota
	// probeDenied は proxy の拒否応答 (403 と body の proxyDenial) が返った。
	probeDenied
	// probeFailed は HTTP 応答が無い (curl の失敗)。
	probeFailed
)

type probe struct {
	result probeResult
	// code は状態コード、detail は body の先頭か curl の error。
	code, detail string
}

// probeHTTPS は VM 内から https://host/ へ GET を 1 回送る。exec 自体の失敗は error で返す。
func probeHTTPS(ctx context.Context, v vm, host string) (probe, error) {
	out, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"sh", "-c", probeScript, "sh", "https://" + host + "/"}})
	if err != nil {
		return probe{}, fmt.Errorf("VM から %s へ送れない: %w", host, err)
	}
	head, rest, _ := strings.Cut(string(out), "\n")
	status, code, _ := strings.Cut(strings.TrimSpace(head), " ")
	p := probe{code: code, detail: strings.TrimSpace(rest)}
	switch {
	case status != "0" || code == "" || code == "000":
		p.result = probeFailed
	case code == "403" && strings.Contains(rest, proxyDenial):
		p.result = probeDenied
	default:
		p.result = probeReached
	}
	return p, nil
}

// checkEgress は egress 自己検証: VM 内から、許可先に届くことと許可外に届かないことを 1 往復ずつ確かめる
// (decision/0006・0007)。許可集合は global rule の期待集合と sandbox スコープ rule の宛先。probe 先が無い側は省いて表示する。
func checkEgress(ctx context.Context, v vm, prepared preparation, progress io.Writer) error {
	allowed := append(slices.Clone(prepared.GlobalEgress), prepared.Declaration.SandboxEgress...)
	return errors.Join(checkAllowed(ctx, v, allowed, progress), checkDenied(ctx, v, allowed, progress))
}

func checkAllowed(ctx context.Context, v vm, allowed []string, progress io.Writer) error {
	host, ok := egress.AllowedProbe(allowed)
	if !ok {
		logf(progress, "egress 自己検証: glob を含まず 443 を通す許可先が無いので、許可先に届くかの確認を省いた\n")
		return nil
	}
	p, err := probeHTTPS(ctx, v, host)
	switch {
	case err != nil:
		return err
	case p.result == probeDenied:
		return fmt.Errorf("許可先 %s:443 に届かない (proxy が拒否した。sbxr policy sync で global rule を宣言に揃えたか確かめる)", host)
	case p.result == probeFailed:
		return fmt.Errorf("許可先 %s:443 に届かない: %s", host, p.detail)
	}
	logf(progress, "egress 自己検証: 許可先 %s:443 に届いた (%s)\n", host, p.code)
	return nil
}

func checkDenied(ctx context.Context, v vm, allowed []string, progress io.Writer) error {
	host, ok := egress.DeniedProbe(allowed)
	if !ok {
		logf(progress, "egress 自己検証: 許可外の候補がすべて許可されているので、許可外に届かないかの確認を省いた\n")
		return nil
	}
	p, err := probeHTTPS(ctx, v, host)
	switch {
	case err != nil:
		return err
	case p.result == probeReached:
		return fmt.Errorf("許可外の %s:443 に届いた (状態コード %s。sbx の global policy が許可外の宛先を拒否していない)", host, p.code)
	case p.result == probeFailed:
		return fmt.Errorf("許可外の %s:443 への通信が proxy の拒否応答を得ずに失敗した (proxy の policy が効いているか確かめられない): %s", host, p.detail)
	}
	logf(progress, "egress 自己検証: 許可外の %s:443 は proxy が拒否した\n", host)
	return nil
}
