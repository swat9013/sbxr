package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"

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
	Version int            `yaml:"version"`
	Profile Profile        `yaml:"profile"`
	Git     GitDeclaration `yaml:"git"`
	// Egress の要素 (宛先グループ) の形式は egress (#3) で決めるまで検査しない。
	Egress map[string]map[string]any `yaml:"egress"`
	Init   []string                  `yaml:"init"`
	Boot   []string                  `yaml:"boot"`
	// SecretDefs の中身 (注入方式・注入先 host 等) は secret パッケージが検査する。
	SecretDefs map[string]map[string]any `yaml:"secret_defs"`
	Secrets    []string                  `yaml:"secrets"`
	Herdr      HerdrDeclaration          `yaml:"herdr"`
}

// HerdrDeclaration は herdr 連携の宣言 (ADR 0007)。default と user スコープだけが書ける。
type HerdrDeclaration struct {
	Enabled *bool   `yaml:"enabled"`
	Version *string `yaml:"version"`
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

// herdrVersionPattern は herdr の release tag の形。VM 内で release の URL に入るので、形を絞る。
var herdrVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Parse は 1 つのスコープの宣言を読み、値とスコープ制限を検証する。
// source は error に載せる出所 (ファイル path など)。未知の key は error にする。
func Parse(scope Scope, source string, data []byte) (Declaration, error) {
	decl, err := decode(data)
	var keys []writtenKey
	if err == nil {
		keys, err = listWrittenKeys(data)
	}
	if err == nil {
		err = errors.Join(checkWrittenValues(keys), decl.validate(), checkScopeRestrictions(scope, keys))
	}
	if err != nil {
		return Declaration{}, fmt.Errorf("%s (%s スコープ): %w", source, scope, err)
	}
	return decl, nil
}

// writtenKey は宣言ファイルに書かれた key。値が null でも書かれたものとして数える。
type writtenKey struct {
	parent string // 入れ子の key なら親の key ("profile" や "egress.github")。top-level の key は空
	key    string
	value  *yaml.Node
	// tableName は制限表で引く名前。egress の group 名は利用者が付けるので "*" に置き換える ("egress.*.enabled")
	tableName string
}

// name は error と制限表に使う key 名 ("profile.model" のように親の key を前に付ける)。
func (k writtenKey) name() string {
	if k.parent == "" {
		return k.key
	}
	return k.parent + "." + k.key
}

// decode は宣言を型へ読み込む。未知の key と 2 つ目以降の document は error にする。
func decode(data []byte) (Declaration, error) {
	var decl Declaration
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	// 空のファイルは io.EOF になる。空宣言として続け、version の欠落で止める
	if err := decoder.Decode(&decl); err != nil && !errors.Is(err, io.EOF) {
		return Declaration{}, err
	}
	// 2 つ目以降の document は黙って捨てずに止める
	if err := decoder.Decode(new(yaml.Node)); !errors.Is(err, io.EOF) {
		return Declaration{}, errors.New("宣言は 1 つの YAML document に書く")
	}
	return decl, nil
}

// listWrittenKeys は書かれた key を top-level と profile / git の 1 段下まで列挙する。
// 型へ読み込んだ後では、null を書いた key と書いていない key を区別できないため YAML node から数える。
func listWrittenKeys(data []byte) ([]writtenKey, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	var keys []writtenKey
	for _, top := range mappingEntries(documentBody(&root), "", "") {
		keys = append(keys, top)
		switch top.key {
		case "profile", "git", "herdr":
			keys = append(keys, mappingEntries(top.value, top.key, top.key)...)
		case "egress":
			for _, group := range mappingEntries(top.value, "egress", "egress") {
				group.tableName = "egress.*"
				keys = append(keys, group)
				keys = append(keys, mappingEntries(group.value, group.name(), "egress.*")...)
			}
		}
	}
	return keys, nil
}

func documentBody(root *yaml.Node) *yaml.Node {
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		return root.Content[0]
	}
	return root
}

// mappingEntries は mapping の key を列挙する。tableParent は制限表で引く名前の親の部分。
func mappingEntries(node *yaml.Node, parent, tableParent string) []writtenKey {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	var entries []writtenKey
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		tableName := key
		if tableParent != "" {
			tableName = tableParent + "." + key
		}
		entries = append(entries, writtenKey{parent: parent, key: key, value: node.Content[i+1], tableName: tableName})
	}
	return entries
}

// checkWrittenValues は書いた key の値の書き忘れ (null) と、profile / git の空文字を止める。
// 型へ読み込むと null と空は「書いていない」と区別できず、黙って下の層の値に落ちるため。
func checkWrittenValues(keys []writtenKey) error {
	var errs []error
	for _, key := range keys {
		switch {
		case key.value.Tag == "!!null":
			errs = append(errs, fmt.Errorf("%s に値が無い", key.name()))
		case key.parent != "" && key.value.Tag == "!!str" && key.value.Value == "":
			errs = append(errs, fmt.Errorf("%s が空", key.name()))
		}
	}
	return errors.Join(errs...)
}

func (d Declaration) validate() error {
	var errs []error
	if d.Version != SchemaVersion {
		errs = append(errs, fmt.Errorf("version: %d を宣言する (読んだ値: %d)", SchemaVersion, d.Version))
	}
	for _, name := range slices.Sorted(maps.Keys(d.Profile.EnabledPlugins)) {
		if !d.Profile.EnabledPlugins[name] {
			errs = append(errs, fmt.Errorf("profile.enabledPlugins.%s: true だけを書ける (additive のみ。下の層の plugin は消せない)", name))
		}
	}
	if d.Herdr.Version != nil && !herdrVersionPattern.MatchString(*d.Herdr.Version) {
		errs = append(errs, fmt.Errorf("herdr.version: %q は v<major>.<minor>.<patch> の形で書く", *d.Herdr.Version))
	}
	for _, list := range []struct {
		key     string
		entries []string
	}{{"init", d.Init}, {"boot", d.Boot}, {"secrets", d.Secrets}} {
		if slices.Contains(list.entries, "") {
			errs = append(errs, fmt.Errorf("%s に空の entry がある", list.key))
		}
	}
	return errors.Join(errs...)
}
