package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestTheScopeTableClassifiesExactlyTheKeysOfTheDeclaration(t *testing.T) {
	keys := declarationKeys(t, reflect.TypeFor[Declaration](), "")

	for _, key := range keys {
		if _, classified := repoScopeTable[key]; !classified && !belowACannotWriteRow(key) {
			t.Errorf("%s は repo が書けるかを表で分類していない", key)
		}
	}
	for row := range repoScopeTable {
		if !slices.Contains(keys, row) {
			t.Errorf("表の %s は宣言の型に無い key", row)
		}
	}
}

// belowACannotWriteRow は key の祖先に「書けない」行があるか。
func belowACannotWriteRow(key string) bool {
	for ancestor := key; strings.Contains(ancestor, "."); {
		ancestor = ancestor[:strings.LastIndex(ancestor, ".")]
		if access, ok := repoScopeTable[ancestor]; ok && access == repoCannotWrite {
			return true
		}
	}
	return false
}

func TestTheScopeTableNeverTakesADottedKeyNameForADeeperKey(t *testing.T) {
	for _, table := range [][]string{
		{"profile.model"},
		{"egress", "*", "allow.enabled"},
		{"herdr.enabled"},
	} {
		if repoCanWrite(table) {
			t.Errorf("repoCanWrite(%q) = true, want a dotted key name never matched to a row", table)
		}
	}
}

// 今の宣言の型では、次の key は decode が先に止める。表の引き方は、型が中身を決めない map (#13 の template の
// inputs など) を足したときの多層防御として、書かれた key の列挙から直接確かめる。

func TestRepoCannotWriteBelowAKeyTheTableAllowsAlone(t *testing.T) {
	err := repoRestrictions(t, "egress:\n  api:\n    rationale:\n      note: hidden\n")

	assertErrorMentions(t, err, "egress.api.rationale.note は repo 宣言には書けない")
}

func TestRepoCannotWriteAKeyWhoseNameHasADot(t *testing.T) {
	err := repoRestrictions(t, "egress:\n  api:\n    allow.enabled: false\n")

	assertErrorMentions(t, err, "egress.api.allow.enabled は repo 宣言には書けない")
}

// repoRestrictions は data に書かれた key を repo スコープの制限表で引く。
func repoRestrictions(t *testing.T, data string) error {
	t.Helper()
	keys, err := listWrittenKeys([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return checkScopeRestrictions(ScopeRepo, keys)
}

func TestListingKeysStopsAtAnAliasThatRefersToItsOwnAncestor(t *testing.T) {
	keys, err := listWrittenKeys([]byte("a: &a\n  b: *a\n"))

	var names []string
	for _, key := range keys {
		names = append(names, key.name())
	}
	// 参照先を 1 度だけ展開し (a.b.b)、その下で自分を指す alias は降りない
	if want := []string{"a", "a.b", "a.b.b"}; err != nil || !slices.Equal(names, want) {
		t.Errorf("keys = %q, error = %v, want %q", names, err, want)
	}
}

// declarationKeys は型が持つ key を、表で引く名前で数える。
// 利用者が名前を付ける map の key は "*" にする。yaml の key 名を tag に書いていない field と、
// 要素が struct の list は数え方が無いので失敗にする。
func declarationKeys(t *testing.T, typ reflect.Type, parent string) []string {
	t.Helper()
	var keys []string
	for field := range typ.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			t.Errorf("%s.%s は yaml の key 名を tag に書いていない", typ, field.Name)
			continue
		}
		key := strings.TrimPrefix(parent+"."+name, ".")
		keys = append(keys, key)
		value := field.Type
		for value.Kind() == reflect.Pointer {
			value = value.Elem()
		}
		switch {
		case value.Kind() == reflect.Struct:
			keys = append(keys, declarationKeys(t, value, key)...)
		case value.Kind() == reflect.Map && value.Elem().Kind() == reflect.Struct:
			keys = append(keys, key+".*")
			keys = append(keys, declarationKeys(t, value.Elem(), key+".*")...)
		case value.Kind() == reflect.Slice && value.Elem().Kind() == reflect.Struct:
			t.Errorf("%s は要素が struct の list。表で引く名前の数え方を足す", key)
		}
	}
	return keys
}
