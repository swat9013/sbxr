package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// SchemaVersion は本 CLI が読める宣言の schema の版。
const SchemaVersion = 1

// Scope は宣言の層。default → user → repo の順に重なる。
type Scope int

const (
	ScopeDefault Scope = iota
	ScopeUser
	ScopeRepo
)

func (s Scope) String() string {
	switch s {
	case ScopeDefault:
		return "default"
	case ScopeUser:
		return "user"
	case ScopeRepo:
		return "repo"
	}
	return fmt.Sprintf("Scope(%d)", int(s))
}

// Declaration は 1 つのスコープの宣言。3 スコープで同じ型を使い、スコープ差は制限表 (restrictions.go) で表す。
// scalar の pointer と map の nil は「そのスコープが書いていない」を表す。
type Declaration struct {
	Version int                    `yaml:"version"`
	Profile Profile                `yaml:"profile"`
	Git     GitDeclaration         `yaml:"git"`
	Egress  map[string]EgressGroup `yaml:"egress"`
	Init    []string               `yaml:"init"`
	Boot    []string               `yaml:"boot"`
	// SecretDefs の中身 (注入方式・注入先 host 等) の schema は secret 配線 (#4) で決めるまで検査しない。
	SecretDefs map[string]map[string]any `yaml:"secret_defs"`
	Secrets    []string                  `yaml:"secrets"`
}

// Profile は agent runtime profile の宣言。VM 内の Claude Code の settings.json へ入る key だけを持つ。
type Profile struct {
	Model                   *string           `yaml:"model"`
	EffortLevel             *string           `yaml:"effortLevel"`
	EnabledPlugins          map[string]bool   `yaml:"enabledPlugins"`
	Language                *string           `yaml:"language"`
	OutputStyle             *string           `yaml:"outputStyle"`
	FeedbackDrafts          *string           `yaml:"feedbackDrafts"`
	AwaySummaryEnabled      *bool             `yaml:"awaySummaryEnabled"`
	AutoMemoryEnabled       *bool             `yaml:"autoMemoryEnabled"`
	AutoDreamEnabled        *bool             `yaml:"autoDreamEnabled"`
	PromptSuggestionEnabled *bool             `yaml:"promptSuggestionEnabled"`
	SpinnerTipsEnabled      *bool             `yaml:"spinnerTipsEnabled"`
	Env                     map[string]string `yaml:"env"`
	ExtraKnownMarketplaces  map[string]any    `yaml:"extraKnownMarketplaces"`
}

// GitDeclaration は VM 内の commit に使う git identity の宣言。
type GitDeclaration struct {
	Name  *string `yaml:"name"`
	Email *string `yaml:"email"`
}

// EgressGroup は同じ理由で許可する宛先のまとまり。
type EgressGroup struct {
	Rationale string   `yaml:"rationale"`
	Allow     []string `yaml:"allow"`
}

// Parse は 1 つのスコープの宣言を読み、値とスコープ制限を検証する。
// source は error に載せる出所 (ファイル path など)。未知の key は error にする。
func Parse(scope Scope, source string, data []byte) (Declaration, error) {
	var decl Declaration
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	// 空のファイルは io.EOF になる。空宣言として続け、version の欠落で止める
	if err := decoder.Decode(&decl); err != nil && !errors.Is(err, io.EOF) {
		return Declaration{}, fmt.Errorf("%s (%s スコープ): %w", source, scope, err)
	}
	if err := errors.Join(decl.validate(), checkScopeRestrictions(scope, decl)); err != nil {
		return Declaration{}, fmt.Errorf("%s (%s スコープ): %w", source, scope, err)
	}
	return decl, nil
}

func (d Declaration) validate() error {
	var errs []error
	if d.Version != SchemaVersion {
		errs = append(errs, fmt.Errorf("version: %d を宣言する (読んだ値: %d)", SchemaVersion, d.Version))
	}
	for key, value := range map[string]*string{
		"profile.model":          d.Profile.Model,
		"profile.effortLevel":    d.Profile.EffortLevel,
		"profile.language":       d.Profile.Language,
		"profile.outputStyle":    d.Profile.OutputStyle,
		"profile.feedbackDrafts": d.Profile.FeedbackDrafts,
		"git.name":               d.Git.Name,
		"git.email":              d.Git.Email,
	} {
		if value != nil && *value == "" {
			errs = append(errs, fmt.Errorf("%s が空", key))
		}
	}
	for name, enabled := range d.Profile.EnabledPlugins {
		if !enabled {
			errs = append(errs, fmt.Errorf("profile.enabledPlugins.%s: false は書けない (additive のみ。下の層の plugin は消せない)", name))
		}
	}
	for _, name := range d.Secrets {
		if name == "" {
			errs = append(errs, errors.New("secrets に空の名前がある"))
		}
	}
	return errors.Join(errs...)
}
