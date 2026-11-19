package guide

import (
	"regexp"
	"testing"
)

// 案内は利用者の手元の binary だけで読まれるので、repo の中にしか無いものを指せない。
func TestTheGuidePointsAtNothingOutsideTheUsersHands(t *testing.T) {
	for _, spelling := range []*regexp.Regexp{
		regexp.MustCompile(`docs/`),
		regexp.MustCompile(`decision/`),
		regexp.MustCompile(`\bADR\b`),
		regexp.MustCompile(`internal/`),
		regexp.MustCompile(`#[0-9]+`),
		regexp.MustCompile(`README`),
		regexp.MustCompile(`CONTRIBUTING`),
		regexp.MustCompile(`CONTEXT\.md`),
		regexp.MustCompile(`skills/`),
	} {
		if found := spelling.FindString(Text); found != "" {
			t.Errorf("the guide spells %q, which points at something the user does not have", found)
		}
	}
}
