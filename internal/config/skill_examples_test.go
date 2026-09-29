package config

import (
	"os"
	"strings"
	"testing"
)

// skillExamples は skill sbxr-config の SKILL.md から、見出し (## user 設定 / ## repo 宣言) ごとに yaml の例を取り出す。
func skillExamples(t *testing.T) map[string][]string {
	t.Helper()
	data, err := os.ReadFile("../../skills/sbxr-config/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	examples := map[string][]string{}
	heading, block, inBlock := "", []string(nil), false
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSuffix(line, "\n")
		switch {
		case !inBlock && strings.HasPrefix(line, "## "):
			heading = strings.TrimPrefix(line, "## ")
		case !inBlock && line == "```yaml":
			inBlock, block = true, nil
		case inBlock && line == "```":
			inBlock = false
			examples[heading] = append(examples[heading], strings.Join(block, "\n")+"\n")
		case inBlock:
			block = append(block, line)
		}
	}
	if inBlock {
		t.Fatalf("SKILL.md has a yaml example that is not closed under ## %s", heading)
	}
	return examples
}

func TestTheSkillExamplesPassTheValidationOfTheirScope(t *testing.T) {
	examples := skillExamples(t)
	for _, tt := range []struct {
		heading string
		scope   Scope
	}{
		{heading: "user 設定", scope: ScopeUser},
		{heading: "repo 宣言", scope: ScopeRepo},
	} {
		t.Run(tt.heading, func(t *testing.T) {
			if len(examples[tt.heading]) == 0 {
				t.Fatalf("SKILL.md has no yaml example under ## %s", tt.heading)
			}
			for _, example := range examples[tt.heading] {
				_, err := Parse(tt.scope, "SKILL.md の "+tt.heading+" の例", []byte(example))

				if err != nil {
					t.Errorf("the example does not pass: %v\n%s", err, example)
				}
			}
		})
	}
}
