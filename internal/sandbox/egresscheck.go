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

// probeImage は VM の中の container からの確認に使う image。probe の script の出力の形が変わらないよう、
// tag は #135 の実測で使ったものに固定する (decision/0030)。
const probeImage = "curlimages/curl:8.11.1"

// probeOrigin は probe を送る場所。
type probeOrigin struct {
	// prefix は probe の script の前に置くコマンド (script・"sh"・URL が後に続く)。
	prefix []string
	// from は表示で、結果の前に置く語 (VM 直下は空)。
	from string
	// cutOffIsDenial は、curl が proxy を使わない通信の拒否の形 (probeCutOff) で終わったときに「届かない」と読むか。
	cutOffIsDenial bool
}

var (
	// fromVM は VM 直下。proxy の環境変数のもとで、proxy を明示した経路 (forward) を通る。
	fromVM = probeOrigin{prefix: []string{"sh", "-c"}}
	// fromContainer は VM の中の既定の network の container。proxy の環境変数を持たず、proxy を使わない経路 (transparent) を通る
	// (sbx v0.53.0 の実測。#135)。
	fromContainer = probeOrigin{
		prefix:         []string{"docker", "run", "--rm", "--entrypoint", "sh", probeImage, "-c"},
		from:           "VM の中の container から、",
		cutOffIsDenial: true,
	}
)

// cutOffExits は、proxy を使わない通信を sbx の proxy が拒否したときの curl の exit code
// (6: 名前解決の拒否、35: TLS の切断、52: 空の応答。sbx v0.53.0 の実測。#135)。
var cutOffExits = []string{"6", "35", "52"}

// isProxyDenial は、403 の body が host:443 への proxy の拒否応答かを読む (sbx v0.48.0 の実測。decision/0016)。
// どの rule にも当たらない宛先は承認待ち、deny rule に当たった宛先は local rule の拒否になる。どちらも宛先へは届いていない。
// 宛先自身の 403 と取り違えないよう、body の 1 行目を宛先まで含めて一致で照合する (前置きの一致では :4430 も当たる)。
func isProxyDenial(host, body string) bool {
	target := host + ":443"
	first, _, _ := strings.Cut(body, "\n")
	return first == "Approval required for "+target+"." || first == "Blocked by local rule for "+target
}

// probeResult は 1 つの宛先へ送った結果。
type probeResult int

const (
	// probeReached は proxy の拒否応答でない HTTP 応答が返った (状態コードを問わない)。
	probeReached probeResult = iota
	// probeDenied は proxy の拒否応答 (403 と、isProxyDenial に当たる body) が返った。
	probeDenied
	// probeCutOff は HTTP 応答が無く、curl が cutOffExits のどれかで終わった (proxy を使わない通信の拒否の形)。
	probeCutOff
	// probeFailed は HTTP 応答が無い (probeCutOff でない curl の失敗)。
	probeFailed
)

type probe struct {
	result probeResult
	// status は curl の exit code、code は状態コード、detail は body の先頭か curl の error。
	status, code, detail string
}

// probeHTTPS は origin から https://host/ へ GET を 1 回送る。exec 自体の失敗は error で返す。
func probeHTTPS(ctx context.Context, v vm, origin probeOrigin, host string) (probe, error) {
	args := append(slices.Clone(origin.prefix), probeScript, "sh", "https://"+host+"/")
	out, err := v.run(ctx, runtime.SandboxCommand{Args: args})
	if err != nil {
		return probe{}, fmt.Errorf("%s%s へ送れない: %w", origin.sender(), host, err)
	}
	head, rest, _ := strings.Cut(string(out), "\n")
	status, code, _ := strings.Cut(strings.TrimSpace(head), " ")
	p := probe{status: status, code: code, detail: strings.TrimSpace(rest)}
	switch {
	case status != "0" && slices.Contains(cutOffExits, status):
		p.result = probeCutOff
	case status != "0" || code == "" || code == "000":
		p.result = probeFailed
	case code == "403" && isProxyDenial(host, rest):
		p.result = probeDenied
	default:
		p.result = probeReached
	}
	return p, nil
}

// sender は exec の失敗の表示で、送り元を示す語。
func (o probeOrigin) sender() string {
	if o.from == "" {
		return "VM から "
	}
	return o.from
}

// checkEgress は egress 自己検証: 許可先に届くことと許可外に届かないことを、VM 直下と VM の中の container から
// 1 往復ずつ確かめる (decision/0006・0007・0030)。許可集合は global rule の期待集合と sandbox スコープ rule の宛先。
// probe 先が無い側は省いて表示する。VM 直下が通らなければ container は試さない。
func checkEgress(ctx context.Context, v vm, prepared preparation, progress io.Writer) error {
	allowed := append(slices.Clone(prepared.GlobalEgress), prepared.Declaration.SandboxEgress...)
	if err := checkEgressFrom(ctx, v, fromVM, allowed, progress); err != nil {
		return err
	}
	return checkEgressFromContainer(ctx, v, allowed, progress)
}

func checkEgressFrom(ctx context.Context, v vm, origin probeOrigin, allowed []string, progress io.Writer) error {
	return errors.Join(checkAllowed(ctx, v, origin, allowed, progress), checkDenied(ctx, v, origin, allowed, progress))
}

// checkEgressFromContainer は container に使う image を pull してから、container から確かめる。
// probe 先が両側とも無ければ pull もしない。
func checkEgressFromContainer(ctx context.Context, v vm, allowed []string, progress io.Writer) error {
	_, hasAllowed := egress.AllowedProbe(allowed)
	_, hasDenied := egress.DeniedProbe(allowed)
	if !hasAllowed && !hasDenied {
		logf(progress, "egress 自己検証: probe 先が無いので、VM の中の container からの確認を省いた\n")
		return nil
	}
	if _, err := v.run(ctx, runtime.SandboxCommand{Args: []string{"docker", "pull", probeImage}}); err != nil {
		return fmt.Errorf("VM の中の container から確かめられない: image %s を pull できない"+
			" (VM の中の Docker が Docker Hub から pull するには、egress の docker-registry group が要る): %w", probeImage, err)
	}
	return checkEgressFrom(ctx, v, fromContainer, allowed, progress)
}

func checkAllowed(ctx context.Context, v vm, origin probeOrigin, allowed []string, progress io.Writer) error {
	host, ok := egress.AllowedProbe(allowed)
	if !ok {
		logf(progress, "egress 自己検証: glob を含まず 443 を通す許可先が無いので、%s許可先に届くかの確認を省いた\n", origin.from)
		return nil
	}
	p, err := probeHTTPS(ctx, v, origin, host)
	switch {
	case err != nil:
		return err
	case p.result == probeDenied:
		return fmt.Errorf("%s許可先 %s:443 に届かない (proxy が拒否した。sbxr policy sync で global rule を宣言に揃えたか確かめる)", origin.from, host)
	case p.result == probeCutOff, p.result == probeFailed:
		return fmt.Errorf("%s許可先 %s:443 に届かない: %s", origin.from, host, p.detail)
	}
	logf(progress, "egress 自己検証: %s許可先 %s:443 に届いた (%s)\n", origin.from, host, p.code)
	return nil
}

func checkDenied(ctx context.Context, v vm, origin probeOrigin, allowed []string, progress io.Writer) error {
	host, ok := egress.DeniedProbe(allowed)
	if !ok {
		logf(progress, "egress 自己検証: 許可外の候補がすべて許可されているので、%s許可外に届かないかの確認を省いた\n", origin.from)
		return nil
	}
	p, err := probeHTTPS(ctx, v, origin, host)
	switch {
	case err != nil:
		return err
	case p.result == probeReached:
		// body を添えるのは、policy が効いていないのか、proxy の拒否応答の文面が変わったのかを見分けるため
		return fmt.Errorf("%s許可外の %s:443 に届いた (状態コード %s。sbx の global policy が許可外の宛先を拒否していない): %s", origin.from, host, p.code, p.detail)
	case p.result == probeCutOff && origin.cutOffIsDenial:
		logf(progress, "egress 自己検証: %s許可外の %s:443 は proxy が拒否した (curl の exit %s)\n", origin.from, host, p.status)
		return nil
	case p.result == probeCutOff, p.result == probeFailed:
		return fmt.Errorf("%s許可外の %s:443 への通信が proxy の拒否%sを得ずに失敗した (proxy の policy が効いているか確かめられない): %s",
			origin.from, host, denialForms(origin), p.detail)
	}
	logf(progress, "egress 自己検証: %s許可外の %s:443 は proxy が拒否した\n", origin.from, host)
	return nil
}

// denialForms は「届かない」と読む形を、拒否を得ずに失敗したときの表示に添える。
func denialForms(origin probeOrigin) string {
	if !origin.cutOffIsDenial {
		return "応答"
	}
	return " (拒否応答か、curl の exit " + strings.Join(cutOffExits, "・") + ")"
}
