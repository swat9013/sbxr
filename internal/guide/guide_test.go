package guide

import (
	"regexp"
	"testing"
)

// 案内は利用者の手元の binary だけで読まれるので、sbxr の repo の中にしか無いものを指せない。
// 利用者の repo にもありうる一般の名前 (README・docs/ など) は、案内が対象 repo の file を指すのに使うので止めない。
func TestTheGuidePointsAtNothingOutsideTheUsersHands(t *testing.T) {
	for _, spelling := range []*regexp.Regexp{
		regexp.MustCompile(`docs/(design|adr|agents)`),
		regexp.MustCompile(`(?i)\bdecision\b`),
		regexp.MustCompile(`(?i)\bADR\b`),
		regexp.MustCompile(`internal/`),
		regexp.MustCompile(`#[0-9]+`),
		regexp.MustCompile(`CONTRIBUTING`),
		regexp.MustCompile(`CONTEXT\.md`),
		regexp.MustCompile(`CLAUDE\.md`),
		regexp.MustCompile(`\.claude/`),
		regexp.MustCompile(`skills/`),
	} {
		if found := spelling.FindString(Text); found != "" {
			t.Errorf("the guide spells %q, which points at something the user does not have", found)
		}
	}
}
