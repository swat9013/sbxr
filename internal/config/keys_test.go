package config

import (
	"reflect"
	"slices"
	"testing"
)

func TestEveryKeyOfTheDeclarationTypeIsCounted(t *testing.T) {
	if len(uncountableKeys) > 0 {
		t.Errorf("uncountable keys = %q, want every key of the declaration type counted", uncountableKeys)
	}
}

func TestCountingKeysReportsAFieldWithoutAYAMLKeyName(t *testing.T) {
	type declaration struct {
		Named    string `yaml:"named"`
		Untagged string
	}

	_, uncountable := countDeclarationKeys(reflect.TypeFor[declaration]())

	if want := []string{"config.declaration.Untagged"}; !slices.Equal(uncountable, want) {
		t.Errorf("uncountable = %q, want %q", uncountable, want)
	}
}

func TestCountingKeysReportsAListOfStructs(t *testing.T) {
	type item struct {
		Name string `yaml:"name"`
	}
	type declaration struct {
		Items []item `yaml:"items"`
	}

	keys, uncountable := countDeclarationKeys(reflect.TypeFor[declaration]())

	if !slices.Equal(keys, []string{"items"}) || !slices.Equal(uncountable, []string{"items"}) {
		t.Errorf("keys = %q, uncountable = %q, want the list itself counted and its items reported", keys, uncountable)
	}
}

func TestCountingKeysNamesTheKeysOfAUserNamedMapWithAStar(t *testing.T) {
	type group struct {
		Allow []string `yaml:"allow"`
	}
	type declaration struct {
		Groups map[string]group  `yaml:"groups"`
		Env    map[string]string `yaml:"env"`
		Inner  *struct {
			Name string `yaml:"name"`
		} `yaml:"inner"`
	}

	keys, _ := countDeclarationKeys(reflect.TypeFor[declaration]())

	if want := []string{"groups", "groups.*", "groups.*.allow", "env", "inner", "inner.name"}; !slices.Equal(keys, want) {
		t.Errorf("keys = %q, want %q", keys, want)
	}
}
