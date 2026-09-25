package config

import (
	"errors"
	"fmt"
)

// repoForbiddenKeys はスコープ制限表のうち、repo 宣言 (untrusted) が書けない key。
// default と user (信頼済み) は制限を持たない。repo の egress は禁止ではなく、Merge が sandbox スコープ rule として別に置く。
var repoForbiddenKeys = []struct {
	key      string
	declared func(Declaration) bool
}{
	// base 固定の profile key: VM 内 agent の振る舞いの土台で、repo に変えさせない
	{"profile.language", func(d Declaration) bool { return d.Profile.Language != nil }},
	{"profile.outputStyle", func(d Declaration) bool { return d.Profile.OutputStyle != nil }},
	{"profile.feedbackDrafts", func(d Declaration) bool { return d.Profile.FeedbackDrafts != nil }},
	{"profile.awaySummaryEnabled", func(d Declaration) bool { return d.Profile.AwaySummaryEnabled != nil }},
	{"profile.autoMemoryEnabled", func(d Declaration) bool { return d.Profile.AutoMemoryEnabled != nil }},
	{"profile.autoDreamEnabled", func(d Declaration) bool { return d.Profile.AutoDreamEnabled != nil }},
	{"profile.promptSuggestionEnabled", func(d Declaration) bool { return d.Profile.PromptSuggestionEnabled != nil }},
	{"profile.spinnerTipsEnabled", func(d Declaration) bool { return d.Profile.SpinnerTipsEnabled != nil }},
	{"profile.env", func(d Declaration) bool { return d.Profile.Env != nil }},
	{"profile.extraKnownMarketplaces", func(d Declaration) bool { return d.Profile.ExtraKnownMarketplaces != nil }},
	// secret の注入先 host を repo に指定させない (ADR 0003)
	{"secret_defs", func(d Declaration) bool { return d.SecretDefs != nil }},
}

func checkScopeRestrictions(scope Scope, decl Declaration) error {
	if scope != ScopeRepo {
		return nil
	}
	var errs []error
	for _, rule := range repoForbiddenKeys {
		if rule.declared(decl) {
			errs = append(errs, fmt.Errorf("%s は repo 宣言には書けない", rule.key))
		}
	}
	return errors.Join(errs...)
}
