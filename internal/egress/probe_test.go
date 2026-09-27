package egress

import "testing"

func TestAllowedProbePicksTheFirstGlobFreeHTTPSResourceInSortedOrder(t *testing.T) {
	for _, tt := range []struct {
		name      string
		resources []string
		want      string
		wantOK    bool
	}{
		{"glob と 443 以外の port を飛ばす", []string{"github.com:443", "**.docker.io:443", "ports.ubuntu.com:80", "crl?.digicert.com"}, "github.com", true},
		{"port の無い宛先は 443 も通す", []string{"github.com:443", "api.github.com"}, "api.github.com", true},
		{"並びは宣言の順によらない", []string{"pypi.org:443", "astral.sh:443"}, "astral.sh", true},
		{"条件に合う宛先が無い", []string{"*.example.com:443", "ports.ubuntu.com:80"}, "", false},
		{"許可集合が空", nil, "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := AllowedProbe(tt.resources)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("AllowedProbe(%q) = %q, %v, want %q, %v", tt.resources, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestDeniedProbeSkipsCandidatesTheAllowSetLetsThrough(t *testing.T) {
	for _, tt := range []struct {
		name      string
		resources []string
		want      string
		wantOK    bool
	}{
		{"最初の候補", []string{"github.com:443"}, "example.com", true},
		{"許可された候補を飛ばす", []string{"example.com:443"}, "example.net", true},
		{"443 を通さない許可は飛ばさない", []string{"example.com:80"}, "example.com", true},
		{"候補がすべて許可されている", []string{"example.com", "example.net:443", "example.org:443"}, "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DeniedProbe(tt.resources)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("DeniedProbe(%q) = %q, %v, want %q, %v", tt.resources, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
