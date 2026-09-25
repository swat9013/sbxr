package egress

import "testing"

func TestAllowsHTTPSMatchesHostsByTheSameGlobAsSbx(t *testing.T) {
	resources := []string{"github.com:443", "**.githubusercontent.com:443", "crl*.digicert.com", "ports.ubuntu.com:80", "*.one.example.com:443"}
	for host, want := range map[string]bool{
		"github.com":                     true,
		"api.github.com":                 false, // github.com は subdomain を含まない
		"raw.githubusercontent.com":      true,
		"a.b.githubusercontent.com":      true,
		"githubusercontent.com":          false, // ** は 1 label 以上
		"crl3.digicert.com":              true,  // port を書かない rule は全 port
		"ports.ubuntu.com":               false, // 80 だけの許可は HTTPS を通さない
		"x.one.example.com":              true,
		"x.y.one.example.com":            false, // * は 1 label
		"evil.com":                       false,
		"github.com.evil.com":            false,
		"raw.githubusercontent.com.evil": false,
	} {
		if got := AllowsHTTPS(resources, host); got != want {
			t.Errorf("AllowsHTTPS(%q) = %v, want %v", host, got, want)
		}
	}
}
