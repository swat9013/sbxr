// Package egress は egress 宣言を実行基盤の rule へ変換し、global rule を宣言へ収束させる。
package egress

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Group は同じ理由で許可する宛先のまとまり (egress 宣言の要素)。
type Group struct {
	Rationale string   `yaml:"rationale"`
	Allow     []string `yaml:"allow"`
	// Enabled に false を書いた group は除外する。書かなければ有効。
	Enabled *bool `yaml:"enabled"`
}

func (g Group) enabled() bool {
	return g.Enabled == nil || *g.Enabled
}

// resourcePattern は allow の 1 entry の書式: sbx が受け付ける host pattern (*・**・?・[] の glob) と任意の :port。
// カンマは sbx が複数の宛先の区切りとして読むので通さない。IP と CIDR は扱わない。
var resourcePattern = regexp.MustCompile(`^[A-Za-z0-9*?\[\]!-]+(\.[A-Za-z0-9*?\[\]!-]+)+(:\d{1,5})?$`)

// ParseGroups は config が要素を検査せずに持つ egress 宣言を Group へ読み、検証する。
func ParseGroups(raw map[string]map[string]any) (map[string]Group, error) {
	groups := make(map[string]Group, len(raw))
	var errs []error
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		group, err := parseGroup(raw[name])
		if err == nil {
			err = group.validate()
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("egress.%s: %w", name, err))
			continue
		}
		groups[name] = group
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return groups, nil
}

func parseGroup(raw map[string]any) (Group, error) {
	// 未知の field を error にするため、YAML に戻して KnownFields で読み直す
	data, err := yaml.Marshal(raw)
	if err != nil {
		return Group{}, err
	}
	var group Group
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&group); err != nil {
		return Group{}, err
	}
	return group, nil
}

func (g Group) validate() error {
	if !g.enabled() {
		return nil
	}
	var errs []error
	if g.Rationale == "" {
		errs = append(errs, errors.New("rationale が空 (許可する理由を書く)"))
	}
	if len(g.Allow) == 0 {
		errs = append(errs, errors.New("allow が空"))
	}
	for _, resource := range g.Allow {
		host, _, _ := strings.Cut(resource, ":")
		switch {
		case host == "*" || host == "**":
			errs = append(errs, fmt.Errorf("allow の %q は全 host の許可になるので書けない", resource))
		case !resourcePattern.MatchString(resource):
			errs = append(errs, fmt.Errorf("allow の %q は host[:port] の書式でない (URL・path・カンマ区切りは書けない)", resource))
		}
	}
	return errors.Join(errs...)
}

// DesiredResources は有効な group の allow を重複なく並べる。これが global rule の期待集合になる。
func DesiredResources(groups map[string]Group) []string {
	var resources []string
	for _, group := range groups {
		if group.enabled() {
			resources = append(resources, group.Allow...)
		}
	}
	slices.Sort(resources)
	return slices.Compact(resources)
}
