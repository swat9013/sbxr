package egress

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/inmemory"
)

// --- 宣言の検証 ---

func TestValidateRejectsResourcesOutsideTheHostPortFormat(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		needle   string
	}{
		{name: "全 host の許可 **", resource: "**", needle: "host[:port]"},
		{name: "全 host の許可 *", resource: "*", needle: "host[:port]"},
		{name: "URL", resource: "https://x.example.com/path", needle: "host[:port]"},
		{name: "カンマで複数の宛先を 1 entry に詰める", resource: "a.example.com,b.example.com", needle: "host[:port]"},
		{name: "wildcard だけの広すぎる宛先", resource: "**.*:443", needle: "host[:port]"},
		{name: "TLD 全体の wildcard", resource: "**.com:443", needle: "host[:port]"},
		{name: "範囲外の port", resource: "api.example.com:99999", needle: "1〜65535"},
		{name: "port 0", resource: "api.example.com:0", needle: "1〜65535"},
		{name: "大文字の host", resource: "GitHub.com:443", needle: "小文字"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := GroupDeclaration{Allow: []string{tt.resource}}.Validate()

			if err == nil || !strings.Contains(err.Error(), tt.needle) {
				t.Errorf("Validate() error = %v, want an error mentioning %q", err, tt.needle)
			}
		})
	}
}

func TestValidateAcceptsSbxHostPatterns(t *testing.T) {
	allow := []string{"github.com:443", "*.example.com", "**.github.com:443", "crl*.digicert.com:80", "api?.example.com", "api[12].example.com"}

	if err := (GroupDeclaration{Allow: allow}).Validate(); err != nil {
		t.Errorf("Validate() error = %v, want sbx の host pattern を受け入れる", err)
	}
}

func TestCompleteRequiresARationaleAndAllow(t *testing.T) {
	for name, tt := range map[string]struct {
		group  GroupDeclaration
		needle string
	}{
		"rationale が無い":  {group: GroupDeclaration{Allow: []string{"x.example.com:443"}}, needle: "rationale"},
		"allow が空":       {group: GroupDeclaration{Rationale: ptr("x")}, needle: "allow"},
		"除外だけを書いた group": {group: GroupDeclaration{Enabled: ptr(false)}, needle: "rationale"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tt.group.Complete()

			if err == nil || !strings.Contains(err.Error(), tt.needle) {
				t.Errorf("Complete() error = %v, want an error mentioning %q", err, tt.needle)
			}
		})
	}
}

func TestCompleteKeepsAnExcludedGroupWithItsContents(t *testing.T) {
	group, err := GroupDeclaration{Rationale: ptr("GitHub"), Allow: []string{"github.com:443"}, Enabled: ptr(false)}.Complete()

	if err != nil || group.Enabled {
		t.Errorf("Complete() = %+v, error = %v, want a valid group that is excluded", group, err)
	}
}

func TestOverlayUnionsAllowAndLetsTheUpperScopeOverrideTheRest(t *testing.T) {
	lower := GroupDeclaration{Rationale: ptr("GitHub"), Allow: []string{"github.com:443"}}

	got := lower.Overlay(GroupDeclaration{Allow: []string{"ghe.example.com:443", "github.com:443"}, Enabled: ptr(false)})

	want := GroupDeclaration{Rationale: ptr("GitHub"), Allow: []string{"github.com:443", "ghe.example.com:443"}, Enabled: ptr(false)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Overlay() = %+v, want %+v", got, want)
	}
}

func ptr[T any](v T) *T {
	return &v
}

func TestDesiredResourcesSkipDisabledGroupsAndDeduplicate(t *testing.T) {
	groups := map[string]Group{
		"github": {Allow: []string{"github.com:443", "ghcr.io:443"}, Enabled: true},
		"mirror": {Allow: []string{"github.com:443"}, Enabled: true},
		"apt":    {Allow: []string{"ports.ubuntu.com:80"}},
	}

	got := DesiredResources(groups)

	if want := []string{"ghcr.io:443", "github.com:443"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DesiredResources() = %v, want %v", got, want)
	}
}

// --- 差分 ---

// globalAllow は 1 つの global の allow rule。
func globalAllow(id string, resources ...string) runtime.EgressRule {
	return runtime.EgressRule{ID: id, Decision: runtime.DecisionAllow, Resources: resources}
}

// withRules は global rule が live の実行基盤。
func withRules(live ...runtime.EgressRule) *inmemory.Runtime {
	rt := inmemory.New()
	rt.GlobalRules = live
	return rt
}

func TestDiffKeepsOnlyOneAllowRulePerDeclaredResource(t *testing.T) {
	tests := []struct {
		name       string
		live       []runtime.EgressRule
		desired    []string
		wantRemove []string
		wantAdd    []string
	}{
		{
			name:    "宣言と一致する単一 resource の rule は残す",
			live:    []runtime.EgressRule{globalAllow("r1", "github.com:443")},
			desired: []string{"github.com:443"},
		},
		{
			name:       "宣言に無い rule は消して、足りない宛先を足す",
			live:       []runtime.EgressRule{globalAllow("r1", "evil.example.com:443")},
			desired:    []string{"github.com:443"},
			wantRemove: []string{"r1"},
			wantAdd:    []string{"github.com:443"},
		},
		{
			name:       "複数 resource の rule は宣言と一致していても 1 resource 単位へ置き換える",
			live:       []runtime.EgressRule{globalAllow("r1", "a.example.com:443", "b.example.com:443")},
			desired:    []string{"a.example.com:443", "b.example.com:443"},
			wantRemove: []string{"r1"},
			wantAdd:    []string{"a.example.com:443", "b.example.com:443"},
		},
		{
			name:       "同じ宛先を担う重複 rule は後の方を消す",
			live:       []runtime.EgressRule{globalAllow("r1", "github.com:443"), globalAllow("r2", "github.com:443")},
			desired:    []string{"github.com:443"},
			wantRemove: []string{"r2"},
		},
		{
			name:       "手で足した global の deny rule も宣言に無いので消す",
			live:       []runtime.EgressRule{{ID: "d1", Decision: runtime.DecisionDeny, Resources: []string{"github.com:443"}}},
			desired:    []string{"github.com:443"},
			wantRemove: []string{"d1"},
			wantAdd:    []string{"github.com:443"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := Diff(context.Background(), withRules(tt.live...), tt.desired)

			if err != nil {
				t.Fatalf("Diff() error = %v", err)
			}
			if got := ruleIDs(plan.Remove); !reflect.DeepEqual(got, tt.wantRemove) {
				t.Errorf("Plan.Remove = %v, want %v", got, tt.wantRemove)
			}
			if !reflect.DeepEqual(plan.Add, tt.wantAdd) {
				t.Errorf("Plan.Add = %v, want %v", plan.Add, tt.wantAdd)
			}
		})
	}
}

func TestDiffDoesNotWrite(t *testing.T) {
	live := []runtime.EgressRule{globalAllow("r1", "evil.example.com:443")}
	rt := withRules(slices.Clone(live)...)

	_, err := Diff(context.Background(), rt, []string{"github.com:443"})

	if err != nil || !reflect.DeepEqual(rt.GlobalRules, live) {
		t.Errorf("Diff() error = %v, global rules = %+v, want them untouched", err, rt.GlobalRules)
	}
}

// --- 収束 ---

func TestConvergeTwiceMakesNoChangesTheSecondTime(t *testing.T) {
	rt := withRules(globalAllow("r1", "evil.example.com:443"), globalAllow("r2", "a.example.com:443", "github.com:443"))
	desired := []string{"a.example.com:443", "github.com:443"}

	if _, err := Converge(context.Background(), rt, desired); err != nil {
		t.Fatalf("1 回目の Converge() error = %v", err)
	}
	second, err := Converge(context.Background(), rt, desired)

	if err != nil {
		t.Fatalf("2 回目の Converge() error = %v", err)
	}
	if !second.Empty() {
		t.Errorf("2 回目の Converge() = %+v, want no changes", second)
	}
}

func ruleIDs(rules []runtime.EgressRule) []string {
	var ids []string
	for _, r := range rules {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestConvergeKeepsDeclaredHostsReachableWhereverAWriteFails(t *testing.T) {
	desired := []string{"ghcr.io:443", "github.com:443"}
	live := globalAllow("r1", "github.com:443", "ghcr.io:443")
	// 複数宛先の r1 を 1 宛先ずつの rule に置き換えるので、書き込みは足す 2 回と消す 1 回
	for failOn := 1; failOn <= 3; failOn++ {
		t.Run(fmt.Sprintf("書き込み %d 回目で失敗", failOn), func(t *testing.T) {
			rt := withRules(live)
			rt.FailOnGlobalWrite = failOn

			_, err := Converge(context.Background(), rt, desired)

			if err == nil {
				t.Fatalf("Converge() = nil, want the write to fail")
			}
			for _, resource := range desired {
				if !slices.ContainsFunc(rt.GlobalRules, func(r runtime.EgressRule) bool { return slices.Contains(r.Resources, resource) }) {
					t.Errorf("global rules = %+v, want %s still allowed", rt.GlobalRules, resource)
				}
			}
		})
	}
}

func TestConvergeReportsTheChangesMadeBeforeAWriteFails(t *testing.T) {
	rt := withRules(globalAllow("r1", "evil.example.com:443"))
	rt.FailOnGlobalWrite = 2

	applied, err := Converge(context.Background(), rt, []string{"a.example.com:443", "b.example.com:443"})

	if err == nil {
		t.Fatal("Converge() error = nil, want the second write to fail")
	}
	if want := []string{"a.example.com:443"}; !reflect.DeepEqual(applied.Add, want) || len(applied.Remove) != 0 {
		t.Errorf("Converge() applied = %+v, want only the add made before the failure", applied)
	}
}

func TestConvergeFailsWhenTheReadBackStillDiffers(t *testing.T) {
	rt := inmemory.New()
	rt.DropGlobalWrites = true

	_, err := Converge(context.Background(), rt, []string{"github.com:443"})

	if err == nil || !strings.Contains(err.Error(), "適用後") {
		t.Errorf("Converge() error = %v, want a read-back failure", err)
	}
}
