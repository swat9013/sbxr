package guide

import (
	"regexp"
	"testing"
)

// 案内は利用者の手元の binary だけで読まれるので、sbxr の repo の中にしか無いもの (文書・設計 doc・ADR・issue 番号・internal/) を指せない。
// 綴りは sbxr の repo の置き場と名前で止める。案内が対象 repo の同じ名前の file (README・docs/ など) を指す必要が出たら、そのときに絞る。
func TestTheGuidePointsAtNothingOutsideTheUsersHands(t *testing.T) {
	for _, spelling := range []*regexp.Regexp{
		regexp.MustCompile(`README`),
		regexp.MustCompile(`CONTRIBUTING`),
		regexp.MustCompile(`CONTEXT\.md`),
		regexp.MustCompile(`docs/`),
		regexp.MustCompile(`decision/`),
		regexp.MustCompile(`\bADR\b`),
		regexp.MustCompile(`#[0-9]+`),
		regexp.MustCompile(`internal/`),
		regexp.MustCompile(`cmd/`),
		regexp.MustCompile(`skills/`),
	} {
		if found := spelling.FindString(Text); found != "" {
			t.Errorf("the guide spells %q, which points at something the user does not have", found)
		}
	}
}
