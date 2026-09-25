package egress

import (
	"context"
	"fmt"
	"slices"

	"github.com/swat9013/sbxr/internal/runtime"
)

// Plan は global rule を期待集合へ収束させるための変更。
type Plan struct {
	Remove []runtime.EgressRule
	Add    []string
}

// Empty は変更が無いかを返す。
func (p Plan) Empty() bool {
	return len(p.Remove) == 0 && len(p.Add) == 0
}

// Diff は global rule と期待集合の差分を返す。実行基盤には書き込まない。
func Diff(ctx context.Context, rt runtime.Runtime, desired []string) (Plan, error) {
	live, err := rt.ListGlobalEgressRules(ctx)
	if err != nil {
		return Plan{}, err
	}
	return plan(live, desired), nil
}

// Converge は global rule を期待集合へ収束させ、行った変更を返す。
// 適用後に読み直して一致しなければ error にする。
func Converge(ctx context.Context, rt runtime.Runtime, desired []string) (Plan, error) {
	changes, err := Diff(ctx, rt, desired)
	if err != nil {
		return Plan{}, err
	}
	for _, rule := range changes.Remove {
		if err := rt.RemoveGlobalEgressRule(ctx, rule.ID); err != nil {
			return Plan{}, err
		}
	}
	for _, resource := range changes.Add {
		if err := rt.AllowGlobalEgress(ctx, resource); err != nil {
			return Plan{}, err
		}
	}
	remaining, err := Diff(ctx, rt, desired)
	if err != nil {
		return Plan{}, err
	}
	if !remaining.Empty() {
		return Plan{}, fmt.Errorf("適用後も宣言と一致しない (消し残し %d / 足りない宛先 %d)", len(remaining.Remove), len(remaining.Add))
	}
	return changes, nil
}

// plan は残す rule を「allow で 1 resource、期待集合にあり、他の rule がまだ担っていない」ものに限る。
// それ以外 (期待外・deny・複数 resource・重複) は消し、担い手を失った宛先を足す。
func plan(live []runtime.EgressRule, desired []string) Plan {
	var changes Plan
	covered := map[string]bool{}
	for _, rule := range live {
		keep := rule.Decision == "allow" && len(rule.Resources) == 1 &&
			slices.Contains(desired, rule.Resources[0]) && !covered[rule.Resources[0]]
		if keep {
			covered[rule.Resources[0]] = true
			continue
		}
		changes.Remove = append(changes.Remove, rule)
	}
	for _, resource := range desired {
		if !covered[resource] && !slices.Contains(changes.Add, resource) {
			changes.Add = append(changes.Add, resource)
		}
	}
	slices.Sort(changes.Add)
	return changes
}
