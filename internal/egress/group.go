// Package egress は egress 宣言を実行基盤の rule へ変換し、global rule を宣言へ収束させる。
package egress

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
)

// GroupDeclaration は 1 つのスコープが書いた宛先グループ (egress 宣言の要素)。上の層は既存の group に一部の field だけを
// 重ねられる (除外の enabled: false など) ので、書いていない field は nil / 空のまま持つ。
type GroupDeclaration struct {
	Rationale *string  `yaml:"rationale"`
	Allow     []string `yaml:"allow"`
	// Enabled に false を書いた group は除外する。書かなければ有効。
	Enabled *bool `yaml:"enabled"`
}

// Group は層を重ね終えた、中身の揃った宛先グループ。
type Group struct {
	Allow   []string
	Enabled bool
}

// resourcePattern は allow の 1 entry の書式: sbx が受け付ける host pattern (*・**・?・[] の glob) と任意の :port。
// 末尾の 2 label は glob を含まない (「**.com」のような広すぎる指定を通さない)。host は小文字だけにし、
// sbx が保存する形と宣言を文字列で突き合わせられるようにする。カンマは sbx が複数の宛先の区切りとして読むので通さない。
// IPv4 は host と同じ書式として通り、IPv6 と CIDR は扱わない (ADR 0008)。
var resourcePattern = regexp.MustCompile(`^([a-z0-9*?\[\]!-]+\.)*[a-z0-9-]+\.[a-z0-9-]+(:(\d{1,5}))?$`)

// Validate は 1 つのスコープが書いた group の書式 (allow の宛先の書式) を検証する。
// 中身が揃っているかは、層を重ねた後に Complete が確かめる (1 つの層では決まらない)。
func (d GroupDeclaration) Validate() error {
	var errs []error
	for _, resource := range d.Allow {
		match := resourcePattern.FindStringSubmatch(resource)
		switch {
		case match == nil:
			errs = append(errs, fmt.Errorf("allow の %q は host[:port] の書式でない (小文字の host で、末尾の 2 label は glob を含まない。URL・path・カンマ区切りは書けない)", resource))
		case match[3] != "" && !validPort(match[3]):
			errs = append(errs, fmt.Errorf("allow の %q の port は 1〜65535", resource))
		}
	}
	return errors.Join(errs...)
}

// Overlay は下の層の group に上の層の group を重ねる。allow は下の層の後ろに上の層の要素を重複なく足した和集合、
// それ以外は上の層が書いた field が勝つ。上の層が宛先だけを足したり、除外の enabled だけを重ねたりできるようにするため。
func (d GroupDeclaration) Overlay(upper GroupDeclaration) GroupDeclaration {
	allow := slices.Clone(d.Allow)
	for _, resource := range upper.Allow {
		if !slices.Contains(allow, resource) {
			allow = append(allow, resource)
		}
	}
	overlaid := GroupDeclaration{Rationale: d.Rationale, Allow: allow, Enabled: d.Enabled}
	if upper.Rationale != nil {
		overlaid.Rationale = upper.Rationale
	}
	if upper.Enabled != nil {
		overlaid.Enabled = upper.Enabled
	}
	return overlaid
}

// Complete は層を重ねた group に rationale と allow が揃っていることを確かめる。除外した group にも求める。
// 除外は既存の group に enabled: false を重ねて書くので、中身の無い group は除外したい group の名前の書き違いになる (ADR 0008)。
func (d GroupDeclaration) Complete() (Group, error) {
	var errs []error
	if d.Rationale == nil || *d.Rationale == "" {
		errs = append(errs, errors.New("rationale が空 (許可する理由を書く)"))
	}
	if len(d.Allow) == 0 {
		errs = append(errs, errors.New("allow が空"))
	}
	if err := errors.Join(errs...); err != nil {
		return Group{}, err
	}
	return Group{Allow: d.Allow, Enabled: d.Enabled == nil || *d.Enabled}, nil
}

func validPort(digits string) bool {
	port, err := strconv.Atoi(digits)
	return err == nil && port >= 1 && port <= 65535
}

// DesiredResources は有効な group の allow を重複なく並べる。これが global rule の期待集合になる。
func DesiredResources(groups map[string]Group) []string {
	var resources []string
	for _, group := range groups {
		if group.Enabled {
			resources = append(resources, group.Allow...)
		}
	}
	slices.Sort(resources)
	return slices.Compact(resources)
}
