package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/swat9013/sbxr/internal/egress"
	"github.com/swat9013/sbxr/internal/secret"
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
	Version    int                                `yaml:"version"`
	Profile    Profile                            `yaml:"profile"`
	Git        GitDeclaration                     `yaml:"git"`
	Egress     map[string]egress.GroupDeclaration `yaml:"egress"`
	Init       []string                           `yaml:"init"`
	Boot       []string                           `yaml:"boot"`
	SecretDefs map[string]secret.Definition       `yaml:"secret_defs"`
	Secrets    []string                           `yaml:"secrets"`
	Herdr      HerdrDeclaration                   `yaml:"herdr"`
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
	if err != nil {
		err = explainUnknownKeys(err, data)
	}
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
	path  []string // top-level から辿った key 名の列 ("egress", "github", "enabled")
	value *yaml.Node
	line  int // key を書いた行
}

// name は error に使う key 名 ("profile.model" のように親の key を前に付ける)。
func (k writtenKey) name() string {
	return strings.Join(k.path, ".")
}

// isBelow は k が ancestor の下の key か。
func (k writtenKey) isBelow(ancestor []string) bool {
	return len(k.path) > len(ancestor) && slices.Equal(k.path[:len(ancestor)], ancestor)
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

// unknownFieldPattern は decoder が未知の key に返す文言 ("line 3: field modle not found in type config.Profile")。
var unknownFieldPattern = regexp.MustCompile(`^line ([0-9]+): field (.+) not found in type \S+$`)

// explainUnknownKeys は decoder の未知の key の error を、key の path と直し方を持つ文言に言い直す。他の error はそのまま残す。
// decoder の文言は key の名前しか持たず、どの親の下の key か (profile.modle か egress.api.modle か) を読み手が探すことになるため。
func explainUnknownKeys(err error, data []byte) error {
	var typeErr *yaml.TypeError
	if !errors.As(err, &typeErr) {
		return err
	}
	keys, listErr := listWrittenKeys(data)
	if listErr != nil {
		return err
	}
	errs := make([]error, 0, len(typeErr.Errors))
	for _, message := range typeErr.Errors {
		match := unknownFieldPattern.FindStringSubmatch(message)
		if match == nil {
			errs = append(errs, errors.New(message))
			continue
		}
		line, name := match[1], match[2]
		for _, key := range keys {
			if strconv.Itoa(key.line) == line && key.path[len(key.path)-1] == name {
				name = key.name()
				break
			}
		}
		errs = append(errs, fmt.Errorf("line %s: %s は宣言に無い key (書き違いか、この版の sbxr が読まない key。正しい key 名に直すか消す)", line, name))
	}
	return errors.Join(errs...)
}

// listWrittenKeys は書かれた key を、型へ読み込む前の形ですべての深さまで列挙する (ADR 0004 の改訂)。
// 型へ読み込んだ後では、null を書いた key と書いていない key を区別できないため YAML node から数える。
func listWrittenKeys(data []byte) ([]writtenKey, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	return writtenKeysUnder(documentBody(&root), nil, nil), nil
}

func documentBody(root *yaml.Node) *yaml.Node {
	if root.Kind == yaml.DocumentNode && len(root.Content) == 1 {
		return root.Content[0]
	}
	return root
}

// writtenKeysUnder は node の下に書かれた key を、親を子より先に並べて列挙する (checkScopeRestrictions がこの順に頼る)。
// alias は key でも値でも参照先まで辿る (表が止める key を alias で隠させない)。list の要素の下の key は list の key の下として数える。
// expanding は展開中の alias の参照先で、自分の祖先を指す alias を無限に降りない
// (循環と過剰な展開は decode が先に止めるが、列挙はその順序に頼らない)。
func writtenKeysUnder(node *yaml.Node, path []string, expanding []*yaml.Node) []writtenKey {
	var keys []writtenKey
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := writtenKey{
				path:  append(slices.Clip(path), resolved(node.Content[i]).Value),
				value: node.Content[i+1],
				line:  node.Content[i].Line,
			}
			keys = append(keys, key)
			keys = append(keys, writtenKeysUnder(key.value, key.path, expanding)...)
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			keys = append(keys, writtenKeysUnder(item, path, expanding)...)
		}
	case yaml.AliasNode:
		if !slices.Contains(expanding, node.Alias) {
			keys = writtenKeysUnder(node.Alias, path, append(slices.Clip(expanding), node.Alias))
		}
	}
	return keys
}

// resolved は alias を参照先まで辿った node。
func resolved(node *yaml.Node) *yaml.Node {
	for node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	return node
}

// checksEmptyValue は、値の書き忘れ (null) と空文字を止める key か。宣言の型が読む key を止める。
// 型が読まない深さの key (profile.env の変数など) の値は、空でも利用者の意図として通す。
// secret_defs の下は止めない。中身は secret.Definition.Validate が確かめ、空の service は placeholder 注入を表す。
func (k writtenKey) checksEmptyValue() bool {
	if k.isBelow([]string{"secret_defs"}) {
		return false
	}
	return readByDeclaration(declarationKeyOf(k.path))
}

// checkWrittenValues は、checksEmptyValue が選んだ key の値の書き忘れ (null) と、入れ子の key の空文字を止める。
// 型へ読み込むと null と空は「書いていない」と区別できず、黙って下の層の値に落ちるため。
func checkWrittenValues(keys []writtenKey) error {
	var errs []error
	for _, key := range keys {
		value := resolved(key.value)
		switch {
		case !key.checksEmptyValue():
		case value.Tag == "!!null":
			errs = append(errs, fmt.Errorf("%s に値が無い", key.name()))
		case len(key.path) > 1 && value.Tag == "!!str" && value.Value == "":
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
	// egress の group と secret 定義の書式は、書いたファイルの中で確かめる (誤りのファイルとスコープを error に出すため)。
	// group の中身が揃っているかは、層を重ねた後に merge が確かめる
	for _, name := range slices.Sorted(maps.Keys(d.Egress)) {
		if err := d.Egress[name].Validate(); err != nil {
			errs = append(errs, fmt.Errorf("egress.%s: %w", name, err))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(d.SecretDefs)) {
		if err := d.SecretDefs[name].Validate(); err != nil {
			errs = append(errs, fmt.Errorf("secret_defs.%s: %w", name, err))
		}
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
