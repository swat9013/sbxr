package config

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// repoWritableKeys はスコープ制限表のうち、repo 宣言 (untrusted) が書ける key の allowlist。
// ここに無い key を repo が書くと error にする (key を足したときの既定は「repo は書けない」側に倒れる)。
// 表に無いことで表す制限: base 固定の profile key (language・env 等) と secret_defs (注入先 host を repo に指定させない。ADR 0003)。
// default と user (信頼済み) は制限を持たない。repo の egress は Merge が sandbox スコープ rule として別に置く。
var repoWritableKeys = map[string]bool{
	"version":                true,
	"profile":                true,
	"profile.model":          true,
	"profile.effortLevel":    true,
	"profile.enabledPlugins": true,
	"git":                    true,
	"git.name":               true,
	"git.email":              true,
	"egress":                 true,
	"init":                   true,
	"boot":                   true,
	"secrets":                true,
}

func checkScopeRestrictions(scope Scope, decl Declaration, keys []writtenKey) error {
	if scope != ScopeRepo {
		return nil
	}
	var errs []error
	for _, key := range keys {
		if !repoWritableKeys[key.name()] {
			errs = append(errs, fmt.Errorf("%s は repo 宣言には書けない", key.name()))
		}
	}
	// repo の egress は sandbox スコープ rule になり、global rule の group とは重ならない。
	// enabled: false (group の除外) を書いても除外する相手が無いので、書けない key として止める
	for _, name := range slices.Sorted(maps.Keys(decl.Egress)) {
		if _, ok := decl.Egress[name]["enabled"]; ok {
			errs = append(errs, fmt.Errorf("egress.%s.enabled は repo 宣言には書けない (repo の egress は global rule の group を除外できない)", name))
		}
	}
	return errors.Join(errs...)
}
