package config

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
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

func checkScopeRestrictions(scope Scope, decl Declaration) error {
	if scope != ScopeRepo {
		return nil
	}
	var errs []error
	for _, key := range declaredKeys(reflect.ValueOf(decl), "") {
		if !repoWritableKeys[key] {
			errs = append(errs, fmt.Errorf("%s は repo 宣言には書けない", key))
		}
	}
	return errors.Join(errs...)
}

// declaredKeys は宣言が値を書いた key を yaml の key 名で列挙する。Profile と git は field 単位まで降りる。
func declaredKeys(v reflect.Value, prefix string) []string {
	var keys []string
	for i := range v.NumField() {
		field, value := v.Type().Field(i), v.Field(i)
		if value.IsZero() {
			continue
		}
		key := prefix + strings.Split(field.Tag.Get("yaml"), ",")[0]
		keys = append(keys, key)
		if value.Kind() == reflect.Struct {
			keys = append(keys, declaredKeys(value, key+".")...)
		}
	}
	return keys
}
