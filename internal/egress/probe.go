package egress

import (
	"slices"
	"strings"
)

// deniedCandidates は egress 自己検証で「届かない」ことを確かめる宛先の候補 (IANA の予約 domain)。
// 利用者が許可する理由が無く、応答を返し続けるので選んだ (decision/0007)。
var deniedCandidates = []string{"example.com", "example.net", "example.org"}

// AllowedProbe は egress 自己検証で「届く」ことを確かめる host を選ぶ。resources を文字列順に並べ、
// glob を含まず、port が 443 か port の無い最初の宛先の host を返す。無ければ false (decision/0007)。
func AllowedProbe(resources []string) (string, bool) {
	for _, resource := range slices.Sorted(slices.Values(resources)) {
		host, port, hasPort := strings.Cut(resource, ":")
		if hasPort && port != "443" || strings.ContainsAny(host, "*?[") {
			continue
		}
		return host, true
	}
	return "", false
}

// DeniedProbe は egress 自己検証で「届かない」ことを確かめる host を選ぶ。候補を順に見て、
// resources が 443 を通さない最初の 1 つを返す。候補がすべて許可されていれば false。
func DeniedProbe(resources []string) (string, bool) {
	for _, host := range deniedCandidates {
		if !AllowsHTTPS(resources, host) {
			return host, true
		}
	}
	return "", false
}
