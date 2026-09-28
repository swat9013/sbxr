package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestEveryKeyOfTheDeclarationTypeIsListed(t *testing.T) {
	_, unlistable := listDeclarationKeys(reflect.TypeFor[Declaration]())

	if len(unlistable) > 0 {
		t.Errorf("unlistable keys = %q, want every key of the declaration type listed", unlistable)
	}
}

func TestListingKeysReportsAFieldWithoutAYAMLKeyName(t *testing.T) {
	type declaration struct {
		Named    string `yaml:"named"`
		Untagged string
	}

	_, unlistable := listDeclarationKeys(reflect.TypeFor[declaration]())

	if len(unlistable) != 1 || !strings.Contains(unlistable[0], "Untagged") {
		t.Errorf("unlistable = %q, want the field without a yaml key name reported", unlistable)
	}
}

func TestListingKeysListsAListOfStructsButReportsItsItems(t *testing.T) {
	type item struct {
		Name string `yaml:"name"`
	}
	type declaration struct {
		Items []item `yaml:"items"`
	}

	keys, unlistable := listDeclarationKeys(reflect.TypeFor[declaration]())

	if !slices.Equal(keys, []string{"items"}) || !slices.Equal(unlistable, []string{"items"}) {
		t.Errorf("keys = %q, unlistable = %q, want the list itself listed and its items reported", keys, unlistable)
	}
}

func TestListingKeysReportsAListOfPointersToStructs(t *testing.T) {
	type item struct {
		Name string `yaml:"name"`
	}
	type declaration struct {
		Items []*item `yaml:"items"`
	}

	_, unlistable := listDeclarationKeys(reflect.TypeFor[declaration]())

	if want := []string{"items"}; !slices.Equal(unlistable, want) {
		t.Errorf("unlistable = %q, want %q", unlistable, want)
	}
}

func TestListingKeysReportsNestedCollections(t *testing.T) {
	type declaration struct {
		Nested map[string]map[string]string `yaml:"nested"`
		Lists  map[string][]string          `yaml:"lists"`
		Matrix [][]string                   `yaml:"matrix"`
	}

	_, unlistable := listDeclarationKeys(reflect.TypeFor[declaration]())

	if want := []string{"nested", "lists", "matrix"}; !slices.Equal(unlistable, want) {
		t.Errorf("unlistable = %q, want %q", unlistable, want)
	}
}

func TestListingKeysNamesTheKeysOfAUserNamedMapWithAStar(t *testing.T) {
	type group struct {
		Allow []string `yaml:"allow"`
	}
	type declaration struct {
		Groups map[string]*group `yaml:"groups"`
	}

	keys, _ := listDeclarationKeys(reflect.TypeFor[declaration]())

	if want := []string{"groups", "groups.*", "groups.*.allow"}; !slices.Equal(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
}

func TestListingKeysStopsAtAMapOfScalars(t *testing.T) {
	type declaration struct {
		Env map[string]string `yaml:"env"`
	}

	keys, _ := listDeclarationKeys(reflect.TypeFor[declaration]())

	if want := []string{"env"}; !slices.Equal(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
}

func TestListingKeysDescendsIntoAPointerToAStruct(t *testing.T) {
	type declaration struct {
		Inner *struct {
			Name string `yaml:"name"`
		} `yaml:"inner"`
	}

	keys, _ := listDeclarationKeys(reflect.TypeFor[declaration]())

	if want := []string{"inner", "inner.name"}; !slices.Equal(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
}
