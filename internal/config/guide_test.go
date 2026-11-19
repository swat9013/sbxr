package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/guide"
)

// 案内 (sbxr guide) が宣言の型・スコープ制限表とずれないことを確かめる。案内の言い回しは確かめない。

// guideSections は案内を見出し (## ) ごとの行に分ける。
func guideSections() map[string][]string {
	sections := map[string][]string{}
	heading := ""
	for line := range strings.Lines(guide.Text) {
		line = strings.TrimSuffix(line, "\n")
		if strings.HasPrefix(line, "## ") {
			heading = strings.TrimPrefix(line, "## ")
			continue
		}
		sections[heading] = append(sections[heading], line)
	}
	return sections
}

// guideExamples は見出しの下の yaml の例を取り出す。
func guideExamples(t *testing.T, heading string) []string {
	t.Helper()
	var examples, block []string
	inBlock := false
	for _, line := range guideSections()[heading] {
		switch {
		case !inBlock && line == "```yaml":
			inBlock, block = true, nil
		case inBlock && line == "```":
			inBlock = false
			examples = append(examples, strings.Join(block, "\n")+"\n")
		case inBlock:
			block = append(block, line)
		}
	}
	if inBlock {
		t.Fatalf("the guide has a yaml example that is not closed under ## %s", heading)
	}
	return examples
}

// guideKeyRow は案内の key の表の 1 行。
type guideKeyRow struct {
	key           string
	repoCanWrite  bool
	repoCellValid bool
}

// guideKeyRows は ## key の一覧 の表の行を、key の列 (`key`) と repo 宣言の列で読む。
func guideKeyRows(t *testing.T) []guideKeyRow {
	t.Helper()
	var rows []guideKeyRow
	for _, line := range guideSections()["key の一覧"] {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) != 3 {
			t.Fatalf("the guide key table row has %d cells, want key・意味・repo 宣言 (a | in a cell shifts the columns): %s", len(cells), line)
		}
		repoCell := strings.TrimSpace(cells[2])
		rows = append(rows, guideKeyRow{
			key:           strings.Trim(strings.TrimSpace(cells[0]), "`"),
			repoCanWrite:  repoCell == "書ける",
			repoCellValid: repoCell == "書ける" || repoCell == "書けない",
		})
	}
	if len(rows) == 0 {
		t.Fatal("the guide has no key table under ## key の一覧")
	}
	return rows
}

func TestTheGuideKeyTableListsExactlyTheKeysOfTheDeclaration(t *testing.T) {
	var listed []string
	for _, row := range guideKeyRows(t) {
		listed = append(listed, row.key)
	}

	slices.Sort(listed)
	if want := slices.Sorted(slices.Values(declarationKeys)); !slices.Equal(listed, want) {
		t.Errorf("guide key table = %q, want one row for each key of the declaration type %q", listed, want)
	}
}

func TestTheGuideKeyTableTellsWhetherTheRepoCanWriteAsTheScopeTableDoes(t *testing.T) {
	for _, row := range guideKeyRows(t) {
		if want := repoCanWrite(strings.Split(row.key, ".")); !row.repoCellValid || row.repoCanWrite != want {
			t.Errorf("the guide says %s is writable by the repo = %t (cell valid %t), want %t", row.key, row.repoCanWrite, row.repoCellValid, want)
		}
	}
}

func TestTheGuideExamplesPassTheValidationOfTheirScope(t *testing.T) {
	for _, tt := range []struct {
		heading string
		scope   Scope
	}{
		{heading: "user 設定", scope: ScopeUser},
		{heading: "repo 宣言", scope: ScopeRepo},
	} {
		t.Run(tt.heading, func(t *testing.T) {
			examples := guideExamples(t, tt.heading)
			if len(examples) == 0 {
				t.Fatalf("the guide has no yaml example under ## %s", tt.heading)
			}
			for _, example := range examples {
				_, err := Parse(tt.scope, "案内の "+tt.heading+" の例", []byte(example))

				if err != nil {
					t.Errorf("the example does not pass: %v\n%s", err, example)
				}
			}
		})
	}
}
