package config

import (
	"reflect"
	"slices"
	"strings"
)

// declarationKeys は宣言の型が読む key。名前は key を "." でつなぎ、利用者が名前を付ける map の key は "*" にする ("egress.*.allow")。
// 列挙の仕方が無い key が型に無いことは test が確かめる。
var declarationKeys, _ = listDeclarationKeys(reflect.TypeFor[Declaration]())

// listDeclarationKeys は型が読む key を列挙する。yaml の key 名を tag に書いていない field、要素が struct の list、
// 要素が map か list の map と list は列挙の仕方が無いので、黙って飛ばさずに unlistable に挙げる。
func listDeclarationKeys(typ reflect.Type) (keys, unlistable []string) {
	var list func(typ reflect.Type, parent string)
	list = func(typ reflect.Type, parent string) {
		for field := range typ.Fields() {
			name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
			if name == "-" {
				continue
			}
			if name == "" {
				unlistable = append(unlistable, typ.String()+"."+field.Name)
				continue
			}
			key := strings.TrimPrefix(parent+"."+name, ".")
			keys = append(keys, key)
			value := pointee(field.Type)
			switch value.Kind() {
			case reflect.Struct:
				list(value, key)
			case reflect.Map:
				switch elem := pointee(value.Elem()); {
				case elem.Kind() == reflect.Struct:
					keys = append(keys, key+".*")
					list(elem, key+".*")
				case isCollection(elem):
					unlistable = append(unlistable, key)
				}
			case reflect.Slice:
				if elem := pointee(value.Elem()); elem.Kind() == reflect.Struct || isCollection(elem) {
					unlistable = append(unlistable, key)
				}
			}
		}
	}
	list(typ, "")
	return keys, unlistable
}

// isCollection は型が map か list か。
func isCollection(typ reflect.Type) bool {
	return typ.Kind() == reflect.Map || typ.Kind() == reflect.Slice
}

// pointee は pointer を辿った先の型。
func pointee(typ reflect.Type) reflect.Type {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
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
