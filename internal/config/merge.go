package config

import (
	"errors"
	"maps"
	"slices"
)

// Config は 3 スコープを merge した結果。
type Config struct {
	Profile Profile
	Git     Identity
	// GlobalEgress は default と user の egress 宣言。全 sandbox VM に常時適用する global rule になる。
	GlobalEgress map[string]map[string]any
	// SandboxEgress は repo の egress 宣言。その sandbox VM にだけ適用する sandbox スコープ rule になる。
	SandboxEgress map[string]map[string]any
	Init          []string
	Boot          []string
	SecretDefs    map[string]map[string]any
	Secrets       []string
}

// Identity は merge 後の git identity。name と email はどちらも空でない。
type Identity struct {
	Name  string
	Email string
}

// Merge は default → user → repo の順に宣言を重ねる。
// map 系は additive (同じ key は後の層が勝つ)、scalar は override、list は並べて足し、secrets は和集合にする。
// 宣言ファイルが無いスコープには zero 値の Declaration を渡す。
func Merge(defaultDecl, userDecl, repoDecl Declaration) (Config, error) {
	var cfg Config
	var git GitDeclaration
	for _, decl := range []Declaration{defaultDecl, userDecl, repoDecl} {
		cfg.Profile = cfg.Profile.overlay(decl.Profile)
		git = GitDeclaration{Name: override(git.Name, decl.Git.Name), Email: override(git.Email, decl.Git.Email)}
		cfg.Init = append(cfg.Init, decl.Init...)
		cfg.Boot = append(cfg.Boot, decl.Boot...)
		cfg.SecretDefs = additive(cfg.SecretDefs, decl.SecretDefs)
		for _, name := range decl.Secrets {
			if !slices.Contains(cfg.Secrets, name) {
				cfg.Secrets = append(cfg.Secrets, name)
			}
		}
	}
	cfg.GlobalEgress = globalEgress(defaultDecl, userDecl)
	cfg.SandboxEgress = additiveGroups(nil, repoDecl.Egress)

	var errs []error
	if git.Name == nil {
		errs = append(errs, errors.New("git.name がどのスコープにも無い (user 設定か repo 宣言で宣言する)"))
	}
	if git.Email == nil {
		errs = append(errs, errors.New("git.email がどのスコープにも無い (user 設定か repo 宣言で宣言する)"))
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	cfg.Git = Identity{Name: *git.Name, Email: *git.Email}
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

// globalEgress は global rule になる egress 宣言 (default と user) を重ねる。
func globalEgress(defaultDecl, userDecl Declaration) map[string]map[string]any {
	return additiveGroups(additiveGroups(nil, defaultDecl.Egress), userDecl.Egress)
}

// additiveGroups は宛先グループを名前ごとに重ね、同じ名前のグループは中の field ごとに上の層が勝つ。
// user が default のグループの一部の field (例: 除外の enabled) だけを書き換えられるようにするため。
func additiveGroups(lower, upper map[string]map[string]any) map[string]map[string]any {
	if lower == nil && upper == nil {
		return nil
	}
	out := make(map[string]map[string]any, len(lower)+len(upper))
	for name, group := range lower {
		out[name] = additive(nil, group)
	}
	for name, group := range upper {
		out[name] = additive(out[name], group)
	}
	return out
}
