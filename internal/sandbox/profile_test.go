package sandbox

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/swat9013/sbxr/internal/config"
)

// declaredProfile は model と、false を指す autoMemoryEnabled だけを宣言した profile。
func declaredProfile() config.Profile {
	model, off := "opus", false
	return config.Profile{Model: &model, AutoMemoryEnabled: &off}
}

func TestTheSummaryShowsOnlyTheDeclaredProfileKeysIncludingFalse(t *testing.T) {
	prepared := preparation{Declaration: sandboxDeclaration{Profile: declaredProfile()}}

	summary, err := prepared.summary()
	if err != nil {
		t.Fatal(err)
	}

	var view struct {
		Declaration struct {
			Profile map[string]any `yaml:"profile"`
		} `yaml:"declaration"`
	}
	if err := yaml.Unmarshal([]byte(summary), &view); err != nil {
		t.Fatal(err)
	}
	// 宣言していない key を null で見せると、settings.json に null を書くと読める (VM へは入らない)
	want := map[string]any{"model": "opus", "autoMemoryEnabled": false}
	if !reflect.DeepEqual(view.Declaration.Profile, want) {
		t.Errorf("summary profile = %v, want %v\n%s", view.Declaration.Profile, want, summary)
	}
}

func TestProfileSettingsKeepsAKeyDeclaredFalse(t *testing.T) {
	settings, err := profileSettings(declaredProfile())

	want := map[string]any{"model": "opus", "autoMemoryEnabled": false}
	if err != nil || !reflect.DeepEqual(settings, want) {
		t.Errorf("profileSettings() = %v, %v, want %v", settings, err, want)
	}
}

// v010AsCurrent は v0.1.0 の作成時の宣言 (v010Declaration) と同じ内容を、今の宣言として組んだもの。
// v0.1.0 は profile の yaml tag に omitempty が無く、宣言していない key を null・空の map で記録した。
func v010AsCurrent() sandboxDeclaration {
	opus, medium, off, no := "opus", "medium", "off", false
	current := sandboxDeclaration{
		Profile: config.Profile{
			Model: &opus, EffortLevel: &medium, FeedbackDrafts: &off,
			AwaySummaryEnabled: &no, AutoMemoryEnabled: &no, AutoDreamEnabled: &no, PromptSuggestionEnabled: &no, SpinnerTipsEnabled: &no,
			Env: map[string]string{
				"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE":          "60",
				"CLAUDE_BASH_MAINTAIN_PROJECT_WORKING_DIR": "1",
				"CLAUDE_CODE_ENABLE_TODO_TOOLS":            "1",
				"DISABLE_ERROR_REPORTING":                  "1",
			},
			ExtraKnownMarketplaces: map[string]any{
				"claude-plugins-official": map[string]any{"source": map[string]any{"repo": "anthropics/claude-plugins-official", "source": "github"}},
			},
		},
		Herdr: &herdrPin{Version: "v0.9.0"},
	}
	current.Git.Name, current.Git.Email = "tester", "tester@example.com"
	return current
}

func TestARecordWithNullProfileKeysIsComparedByTheDeclaredValues(t *testing.T) {
	data, err := os.ReadFile(v010Declaration)
	if err != nil {
		t.Fatal(err)
	}
	dir := stateDir{path: t.TempDir()}
	if err := os.WriteFile(filepath.Join(dir.path, declarationFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	record, found, err := dir.record()
	if err != nil || !found {
		t.Fatalf("record() = %v, %v", found, err)
	}
	for _, tc := range []struct {
		name   string
		change func(*sandboxDeclaration)
		want   []Difference
	}{
		{"the same declaration", func(*sandboxDeclaration) {}, nil},
		{"a key recorded as null and now declared", func(d *sandboxDeclaration) { ja := "ja"; d.Profile.Language = &ja },
			[]Difference{{Path: "profile.language", Recorded: "(なし)", Current: `"ja"`}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := v010AsCurrent()
			tc.change(&current)

			got, err := record.Drift(current)

			if err != nil || !reflect.DeepEqual(got.Differences, tc.want) {
				t.Errorf("Drift = %v, %v, want %v", got.Differences, err, tc.want)
			}
		})
	}
}
