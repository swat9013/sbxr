package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/swat9013/sbxr/internal/egress"
	"github.com/swat9013/sbxr/internal/secret"
)

// Config は 3 スコープを merge し、検証を終えた結果。
type Config struct {
	Profile Profile
	// git は重ねた git identity。使う側が GitIdentity で揃っていることを確かめる。
	git GitDeclaration
	// GlobalEgress は default と user の egress 宣言の宛先。全 sandbox VM に常時適用する global rule になる。
	GlobalEgress []string
	// SandboxEgress は repo の egress 宣言の宛先。その sandbox VM にだけ適用する sandbox スコープ rule になる。
	SandboxEgress []string
	Init          []string
	Boot          []string
	SecretDefs    map[string]secret.Definition
	Secrets       []string
	Herdr         Herdr
}

// Herdr は merge 後の herdr 連携。Enabled なら Version は空でない。
type Herdr struct {
	Enabled bool
	Version string
}

// Identity は merge 後の git identity。name と email はどちらも空でない。
type Identity struct {
	Name  string
	Email string
}

// GitIdentity は VM 内の commit に使う git identity を返す。どのスコープにも無い key があれば止める。
// global rule と secret 定義だけを使う操作は git identity を要らないので、merge ではなく使う側が確かめる。
func (c Config) GitIdentity() (Identity, error) {
	var errs []error
	if c.git.Name == nil {
		errs = append(errs, errors.New("git.name がどのスコープにも無い (user 設定か repo 宣言で宣言する)"))
	}
	if c.git.Email == nil {
		errs = append(errs, errors.New("git.email がどのスコープにも無い (user 設定か repo 宣言で宣言する)"))
	}
	if err := errors.Join(errs...); err != nil {
		return Identity{}, err
	}
	return Identity{Name: *c.git.Name, Email: *c.git.Email}, nil
}

// Merge は default → user → repo の順に宣言を重ね、層を重ねた後でしか決まらない検証をする。
// map 系は additive (同じ key は後の層が勝つ)、scalar は override、list は並べて足し、secrets は和集合にする。
// 宣言ファイルが無いスコープには zero 値の Declaration を渡す。
func Merge(defaultDecl, userDecl, repoDecl Declaration) (Config, error) {
	var cfg Config
	var herdr HerdrDeclaration
	for _, decl := range []Declaration{defaultDecl, userDecl, repoDecl} {
		herdr = HerdrDeclaration{Enabled: override(herdr.Enabled, decl.Herdr.Enabled), Version: override(herdr.Version, decl.Herdr.Version)}
		cfg.Profile = cfg.Profile.overlay(decl.Profile)
		cfg.git = GitDeclaration{Name: override(cfg.git.Name, decl.Git.Name), Email: override(cfg.git.Email, decl.Git.Email)}
		cfg.Init = append(cfg.Init, decl.Init...)
		cfg.Boot = append(cfg.Boot, decl.Boot...)
		cfg.SecretDefs = additive(cfg.SecretDefs, decl.SecretDefs)
		for _, name := range decl.Secrets {
			if !slices.Contains(cfg.Secrets, name) {
				cfg.Secrets = append(cfg.Secrets, name)
			}
		}
	}

	var globalErr, sandboxErr error
	cfg.GlobalEgress, globalErr = egressResources(scopedEgress{ScopeDefault, defaultDecl.Egress}, scopedEgress{ScopeUser, userDecl.Egress})
	cfg.SandboxEgress, sandboxErr = egressResources(scopedEgress{ScopeRepo, repoDecl.Egress})
	errs := []error{globalErr, sandboxErr}
	if herdr.Enabled != nil && *herdr.Enabled {
		if herdr.Version == nil {
			errs = append(errs, errors.New("herdr.version がどのスコープにも無い (herdr を有効にするには版を宣言する)"))
		} else {
			cfg.Herdr = Herdr{Enabled: true, Version: *herdr.Version}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (p Profile) overlay(upper Profile) Profile {
	return Profile{
		Model:                   override(p.Model, upper.Model),
		EffortLevel:             override(p.EffortLevel, upper.EffortLevel),
		EnabledPlugins:          additive(p.EnabledPlugins, upper.EnabledPlugins),
		Language:                override(p.Language, upper.Language),
		OutputStyle:             override(p.OutputStyle, upper.OutputStyle),
		FeedbackDrafts:          override(p.FeedbackDrafts, upper.FeedbackDrafts),
		AwaySummaryEnabled:      override(p.AwaySummaryEnabled, upper.AwaySummaryEnabled),
		AutoMemoryEnabled:       override(p.AutoMemoryEnabled, upper.AutoMemoryEnabled),
		AutoDreamEnabled:        override(p.AutoDreamEnabled, upper.AutoDreamEnabled),
		PromptSuggestionEnabled: override(p.PromptSuggestionEnabled, upper.PromptSuggestionEnabled),
		SpinnerTipsEnabled:      override(p.SpinnerTipsEnabled, upper.SpinnerTipsEnabled),
		Env:                     additive(p.Env, upper.Env),
		ExtraKnownMarketplaces:  additive(p.ExtraKnownMarketplaces, upper.ExtraKnownMarketplaces),
	}
}

// override は上の層が書いた scalar を採り、書いていなければ下の層の値を残す。
func override[T any](lower, upper *T) *T {
	if upper != nil {
		return upper
	}
	return lower
}

// additive は下の層の map に上の層の entry を足した新しい map を返す (同じ key は上の層が勝つ)。
func additive[M ~map[K]V, K comparable, V any](lower, upper M) M {
	if lower == nil && upper == nil {
		return nil
	}
	out := make(M, len(lower)+len(upper))
	maps.Copy(out, lower)
	maps.Copy(out, upper)
	return out
}

// scopedEgress は 1 つのスコープが書いた egress 宣言。
type scopedEgress struct {
	scope  Scope
	groups map[string]egress.GroupDeclaration
}

// egressResources は egress 宣言の層を宛先グループごとに重ね、中身が揃っていることを確かめて、有効な group の宛先を返す。
// 同じ名前のグループは field ごとに重ね、allow は下の層の後ろに上の層の要素を重複なく足した和集合、それ以外は上の層が勝つ。
// user が default のグループに宛先を足したり、一部の field (例: 除外の enabled) だけを書き換えたりできるようにするため。
// 中身の欠けた group の error には、その group 名を最初に書いたスコープを付ける (除外する group 名の書き違いを、書いた層で指す)。
func egressResources(layers ...scopedEgress) ([]string, error) {
	merged := map[string]egress.GroupDeclaration{}
	origin := map[string]Scope{}
	for _, layer := range layers {
		for name, upper := range layer.groups {
			lower, seen := merged[name]
			if !seen {
				origin[name] = layer.scope
			}
			merged[name] = egress.GroupDeclaration{
				Rationale: override(lower.Rationale, upper.Rationale),
				Allow:     union(lower.Allow, upper.Allow),
				Enabled:   override(lower.Enabled, upper.Enabled),
			}
		}
	}
	groups := make(map[string]egress.Group, len(merged))
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(merged)) {
		group, err := merged[name].Complete()
		if err != nil {
			errs = append(errs, fmt.Errorf("egress.%s (%s スコープが書いた group): %w", name, origin[name], err))
			continue
		}
		groups[name] = group
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return egress.DesiredResources(groups), nil
}

// union は下の層の list の後ろに、上の層の要素を重複なく足す。
func union(lower, upper []string) []string {
	out := slices.Clone(lower)
	for _, item := range upper {
		if !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}
