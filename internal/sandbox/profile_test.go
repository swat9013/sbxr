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

// recordedWithNullProfileKeys は profile の yaml tag に omitempty が無かった版が書いた作成時の宣言の形
// (宣言していない key を null・空の map で書いた)。
const recordedWithNullProfileKeys = `profile:
    model: opus
    effortLevel: null
    enabledPlugins: {}
    language: null
    outputStyle: null
    feedbackDrafts: null
    awaySummaryEnabled: null
    autoMemoryEnabled: false
    autoDreamEnabled: null
    promptSuggestionEnabled: null
    spinnerTipsEnabled: null
    env: {}
    extraKnownMarketplaces: {}
git:
    name: tester
    email: tester@example.com
init: []
boot: []
sandbox_egress: []
secrets: []
`

func TestARecordWithNullProfileKeysIsNoDriftFromTheSameDeclaration(t *testing.T) {
	dir := stateDir{path: t.TempDir()}
	if err := os.WriteFile(filepath.Join(dir.path, declarationFile), []byte(recordedWithNullProfileKeys), 0o600); err != nil {
		t.Fatal(err)
	}
	record, found, err := dir.record()
	if err != nil || !found {
		t.Fatalf("record() = %v, %v", found, err)
	}
	current := sandboxDeclaration{Profile: declaredProfile()}
	current.Git.Name, current.Git.Email = "tester", "tester@example.com"

	got, err := record.Drift(current)

	if err != nil || len(got.Differences) != 0 {
		t.Errorf("Drift = %v, %v, want no difference", got.Differences, err)
	}
}
