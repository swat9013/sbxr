package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestRepoCannotWriteBelowAKeyTheTableAllowsAlone(t *testing.T) {
	repo := "version: 1\negress:\n  api:\n    rationale:\n      note: hidden\n    allow: [api.example.com:443]\n"

	_, err := Parse(ScopeRepo, "repo", []byte(repo))

	assertErrorMentions(t, err, "egress.api.rationale.note は repo 宣言には書けない")
}

func TestRepoCannotHideAKeyBehindAnAlias(t *testing.T) {
	repo := "version: 1\negress:\n  api:\n    rationale: API\n    allow: [&hidden {enabled: false}]\n  github: *hidden\n"

	_, err := Parse(ScopeRepo, "repo", []byte(repo))

	assertErrorMentions(t, err, "egress.github.enabled は repo 宣言には書けない")
}

func TestUserMayLeaveAnEnvValueOfTheProfileEmpty(t *testing.T) {
	if _, err := Parse(ScopeUser, "user", []byte("version: 1\nprofile:\n  env:\n    EMPTY: ''\n")); err != nil {
		t.Errorf("Parse() error = %v, want an empty env value kept as written", err)
	}
}

func TestRepoWritesAnythingBelowAKeyTheTableAllowsWithItsSubtree(t *testing.T) {
	repo := "version: 1\nprofile:\n  enabledPlugins:\n    repo-plugin@mk: true\negress:\n  api:\n    rationale: API\n    allow: [api.example.com:443]\n"

	if _, err := Parse(ScopeRepo, "repo", []byte(repo)); err != nil {
		t.Errorf("Parse() error = %v, want the subtrees written", err)
	}
}

func TestEveryKeyOfTheDeclarationIsClassifiedInTheScopeTable(t *testing.T) {
	for _, key := range declarationKeys(reflect.TypeFor[Declaration](), "") {
		if _, classified := repoScopeTable[key]; !classified {
			t.Errorf("%s は repo が書けるかを表で分類していない", key)
		}
	}
}

// declarationKeys は宣言の型が持つ key を、表で引く名前で数える。利用者が名前を付ける map の key は "*" にする。
func declarationKeys(typ reflect.Type, parent string) []string {
	var keys []string
	for field := range typ.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "" || name == "-" {
			continue
		}
		key := joinKey(parent, name)
		keys = append(keys, key)
		value := field.Type
		for value.Kind() == reflect.Pointer {
			value = value.Elem()
		}
		switch {
		case value.Kind() == reflect.Struct:
			keys = append(keys, declarationKeys(value, key)...)
		case value.Kind() == reflect.Map && value.Elem().Kind() == reflect.Struct:
			keys = append(keys, joinKey(key, "*"))
			keys = append(keys, declarationKeys(value.Elem(), joinKey(key, "*"))...)
		}
	}
	return keys
}
