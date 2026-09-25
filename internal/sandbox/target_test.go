package sandbox

import (
	"slices"
	"testing"
)

func TestCloneCommandChoosesTheCLIByTheURLHost(t *testing.T) {
	for url, tool := range map[string]string{
		"https://github.com/me/app.git":                    "gh",
		"git@github.com:me/app.git":                        "gh",
		"https://gitlab.example.com/me/app.git":            "glab",
		"git@gitlab.com:me/app.git":                        "glab",
		"https://git.example.com/team/gitlab-ci-templates": "git",
		"https://mirror.example.com/github.com/x/y.git":    "git",
		"ssh://git@example.com/me/app.git":                 "git",
	} {
		if got := cloneCommand(url, "/dir"); got[0] != tool || !slices.Contains(got, url) {
			t.Errorf("cloneCommand(%q) = %q, want %s with the URL", url, got, tool)
		}
	}
}
