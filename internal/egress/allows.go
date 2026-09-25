package egress

import (
	"path"
	"strings"
)

// AllowsHTTPS は allow の宛先 (host[:port] の glob) のどれかが host への HTTPS (443) を通すかを返す。
// glob は sbx の読み方に合わせ、label 単位で「*」は 1 label の中、「**」は 1 label 以上に一致させる。
// port を書かない宛先は全 port を通す。
func AllowsHTTPS(resources []string, host string) bool {
	hostLabels := strings.Split(host, ".")
	for _, resource := range resources {
		pattern, port, hasPort := strings.Cut(resource, ":")
		if hasPort && port != "443" {
			continue
		}
		if matchLabels(strings.Split(pattern, "."), hostLabels) {
			return true
		}
	}
	return false
}

func matchLabels(patterns, labels []string) bool {
	if len(patterns) == 0 {
		return len(labels) == 0
	}
	if patterns[0] == "**" {
		for consumed := 1; consumed <= len(labels); consumed++ {
			if matchLabels(patterns[1:], labels[consumed:]) {
				return true
			}
		}
		return false
	}
	if len(labels) == 0 {
		return false
	}
	matched, err := path.Match(patterns[0], labels[0])
	return err == nil && matched && matchLabels(patterns[1:], labels[1:])
}
