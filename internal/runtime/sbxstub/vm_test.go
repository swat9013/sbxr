package sbxstub

import (
	"strings"
	"testing"
)

func TestContainerProbeTurnsEveryProxyDenialIntoTheTransparentDenial(t *testing.T) {
	for name, result := range map[string]HTTPResult{
		"承認待ち":          ProxyDenial("example.com"),
		"deny rule の拒否": {Code: "403", Body: "Blocked by local rule for example.com:443\n"},
	} {
		t.Run(name, func(t *testing.T) {
			vm := &FakeVM{HTTP: map[string]HTTPResult{"example.com": result}}

			out := string(vm.containerProbe("https://example.com/"))

			if !strings.HasPrefix(out, "6 000\n") {
				t.Errorf("container probe = %q, want curl exit 6 as the proxy's transparent path denies it", out)
			}
		})
	}
}
