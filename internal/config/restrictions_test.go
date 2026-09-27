package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/egress"
)

func TestTheScopeTableClassifiesExactlyTheKeysOfTheDeclaration(t *testing.T) {
	keys := declarationKeys(t, reflect.TypeFor[Declaration](), "")
	// egress の group の中身は config が型を持たず、egress が egress.Group へ読む
	keys = append(keys, "egress.*")
	keys = append(keys, declarationKeys(t, reflect.TypeFor[egress.Group](), "egress.*")...)

	for _, key := range keys {
		if _, classified := repoScopeTable[key]; !classified {
			t.Errorf("%s は repo が書けるかを表で分類していない", key)
		}
	}
	for row := range repoScopeTable {
		if !slices.Contains(keys, row) {
			t.Errorf("表の %s は宣言の型に無い key", row)
		}
	}
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
	if !repoCanWrite([]string{"profile", "enabledPlugins", "p@my.marketplace"}) {
		t.Errorf("a dotted key name below a key the table allows with its subtree was rejected")
	}
}

func TestListingKeysStopsAtAnAliasThatRefersToItsOwnAncestor(t *testing.T) {
	keys, err := listWrittenKeys([]byte("a: &a\n  b: *a\n"))

	// 参照先を 1 度だけ展開し (a.b.b)、その下で自分を指す alias は降りない
	if err != nil || len(keys) != 3 {
		t.Errorf("keys = %d, error = %v, want a, a.b and a.b.b", len(keys), err)
	}
}

// declarationKeys は型が持つ key を、表で引く名前で数える。
// yaml の key 名を tag に書いていない field と、要素が struct の map / list は数え方が無いので失敗にする。
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
		case (value.Kind() == reflect.Map || value.Kind() == reflect.Slice) && value.Elem().Kind() == reflect.Struct:
			t.Errorf("%s は要素が struct の map / list。表で引く名前の数え方を足す", key)
		}
	}
	return keys
}
