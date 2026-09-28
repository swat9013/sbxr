package config

import (
	"reflect"
	"slices"
	"strings"
)

// declarationKeys は宣言の型が読む key。名前は key を "." でつなぎ、利用者が名前を付ける map の key は "*" にする ("egress.*.allow")。
// uncountableKeys は数え方が無くて数えられなかった key で、空であることを test が確かめる。
var declarationKeys, uncountableKeys = countDeclarationKeys(reflect.TypeFor[Declaration]())

// countDeclarationKeys は型が読む key を数える。yaml の key 名を tag に書いていない field と、
// 要素が struct の list は数え方が無いので、黙って飛ばさずに uncountable に挙げる。
func countDeclarationKeys(typ reflect.Type) (keys, uncountable []string) {
	var count func(typ reflect.Type, parent string)
	count = func(typ reflect.Type, parent string) {
		for field := range typ.Fields() {
			name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
			if name == "-" {
				continue
			}
			if name == "" {
				uncountable = append(uncountable, typ.String()+"."+field.Name)
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
				count(value, key)
			case value.Kind() == reflect.Map && value.Elem().Kind() == reflect.Struct:
				keys = append(keys, key+".*")
				count(value.Elem(), key+".*")
			case value.Kind() == reflect.Slice && value.Elem().Kind() == reflect.Struct:
				uncountable = append(uncountable, key)
			}
		}
	}
	count(typ, "")
	return keys, uncountable
}

// declarationKeyOf は書かれた key の path を、型が読む key の名前の列にする。
// 型が利用者が名前を付ける map として読む key は "*" に置き換える (["egress", "api", "allow"] → ["egress", "*", "allow"])。
func declarationKeyOf(path []string) []string {
	key := make([]string, 0, len(path))
	for _, name := range path {
		if readByDeclaration(append(slices.Clip(key), "*")) {
			name = "*"
		}
		key = append(key, name)
	}
	return key
}

// readByDeclaration は key (declarationKeyOf の列) を宣言の型が読むか。
func readByDeclaration(key []string) bool {
	name, ok := joinedKeyName(key)
	return ok && slices.Contains(declarationKeys, name)
}

// joinedKeyName は key の列を "." でつないだ名前。名前に "." を含む key は、深い key と取り違えないよう名前を持たない。
func joinedKeyName(key []string) (string, bool) {
	if slices.ContainsFunc(key, func(name string) bool { return strings.Contains(name, ".") }) {
		return "", false
	}
	return strings.Join(key, "."), true
}
