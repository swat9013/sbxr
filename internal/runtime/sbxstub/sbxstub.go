// Package sbxstub は sbx CLI の policy 操作を in-memory で再現する test 用の stub。
// runtime.Sbx にコマンド実行の代わりとして渡し、実 sbx の global rule に触れずに収束を検証する。
package sbxstub

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Rule は sbx policy ls --json が返す rule のうち、sbxr が読む field。
// runtime.Sbx の decoder とは独立に sbx の出力形式を写したもので、両者の食い違いは adapter の test が検出する。
type Rule struct {
	ID           string   `json:"id"`
	Scope        string   `json:"scope"`
	ResourceType string   `json:"resource_type"`
	Decision     string   `json:"decision"`
	Resources    []string `json:"resources"`
	Editable     bool     `json:"editable"`
}

// Stub は sbx の policy の状態と、受け取った書き込みコマンドの記録を持つ。
type Stub struct {
	Rules []Rule
	// Writes は受け取った allow / rm のコマンド (引数を空白で連結したもの)。
	Writes []string
	// DropWrites が true なら書き込みを記録だけして状態に反映しない (適用が効かない sbx の再現)。
	DropWrites bool
	// FailOnWrite が n (1 始まり) なら n 回目の書き込みを失敗させる。0 なら失敗させない。
	FailOnWrite int
	nextID      int
}

// GlobalAllow は scope=global・network・editable の allow rule を作る。
func GlobalAllow(id string, resources ...string) Rule {
	return Rule{ID: id, Scope: "global", ResourceType: "network", Decision: "allow", Resources: resources, Editable: true}
}

// Run は sbx の引数を受け取り、policy ls / allow network / rm network を再現する。
func (s *Stub) Run(_ context.Context, args ...string) ([]byte, error) {
	switch {
	case slices.Equal(args, []string{"policy", "ls", "--json"}):
		// sbx は rule が無くても空の配列を返す
		return json.Marshal(map[string][]Rule{"rules": append([]Rule{}, s.Rules...)})
	case len(args) == 4 && slices.Equal(args[:3], []string{"policy", "allow", "network"}):
		if err := s.recordWrite(args); err != nil {
			return nil, err
		}
		if s.DropWrites {
			return nil, nil
		}
		for _, resource := range strings.Split(args[3], ",") {
			s.nextID++
			s.Rules = append(s.Rules, GlobalAllow(fmt.Sprintf("added-%d", s.nextID), resource))
		}
		return nil, nil
	case len(args) == 5 && slices.Equal(args[:4], []string{"policy", "rm", "network", "--id"}):
		if err := s.recordWrite(args); err != nil {
			return nil, err
		}
		if s.DropWrites {
			return nil, nil
		}
		index := slices.IndexFunc(s.Rules, func(r Rule) bool { return r.ID == args[4] })
		if index < 0 {
			return nil, fmt.Errorf("sbxstub: rule %s が無い", args[4])
		}
		s.Rules = slices.Delete(s.Rules, index, index+1)
		return nil, nil
	}
	return nil, fmt.Errorf("sbxstub: 想定外の引数 %q", args)
}

func (s *Stub) recordWrite(args []string) error {
	if s.FailOnWrite == len(s.Writes)+1 {
		return fmt.Errorf("sbxstub: %d 回目の書き込みを失敗させた", s.FailOnWrite)
	}
	s.Writes = append(s.Writes, strings.Join(args, " "))
	return nil
}
