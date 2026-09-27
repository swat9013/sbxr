package egress

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/inmemory"
)

// --- 宣言の検証 ---

func TestParseGroupsRejectsInvalidDeclarations(t *testing.T) {
	tests := []struct {
		name   string
		group  map[string]any
		needle string
	}{
		{name: "全 host の許可 **", group: map[string]any{"rationale": "x", "allow": []any{"**"}}, needle: "host[:port]"},
		{name: "全 host の許可 *", group: map[string]any{"rationale": "x", "allow": []any{"*"}}, needle: "host[:port]"},
		{name: "URL", group: map[string]any{"rationale": "x", "allow": []any{"https://x.example.com/path"}}, needle: "host[:port]"},
		{name: "カンマで複数の宛先を 1 entry に詰める", group: map[string]any{"rationale": "x", "allow": []any{"a.example.com,b.example.com"}}, needle: "host[:port]"},
		{name: "wildcard だけの広すぎる宛先", group: map[string]any{"rationale": "x", "allow": []any{"**.*:443"}}, needle: "host[:port]"},
		{name: "TLD 全体の wildcard", group: map[string]any{"rationale": "x", "allow": []any{"**.com:443"}}, needle: "host[:port]"},
		{name: "範囲外の port", group: map[string]any{"rationale": "x", "allow": []any{"api.example.com:99999"}}, needle: "1〜65535"},
		{name: "port 0", group: map[string]any{"rationale": "x", "allow": []any{"api.example.com:0"}}, needle: "1〜65535"},
		{name: "大文字の host", group: map[string]any{"rationale": "x", "allow": []any{"GitHub.com:443"}}, needle: "小文字"},
		{name: "rationale が無い", group: map[string]any{"allow": []any{"x.example.com:443"}}, needle: "rationale"},
		{name: "allow が空", group: map[string]any{"rationale": "x"}, needle: "allow"},
		{name: "未知の field", group: map[string]any{"rationale": "x", "allow": []any{"x.example.com"}, "hosts": []any{"y.example.com"}}, needle: "hosts"},
		{name: "enabled が bool でない", group: map[string]any{"rationale": "x", "allow": []any{"x.example.com"}, "enabled": "maybe"}, needle: "into bool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseGroups(map[string]map[string]any{"g": tt.group})

			if err == nil || !strings.Contains(err.Error(), tt.needle) {
				t.Errorf("ParseGroups() error = %v, want an error mentioning %q", err, tt.needle)
			}
		})
	}
}

func TestParseGroupsAcceptsSbxHostPatterns(t *testing.T) {
	allow := []any{"github.com:443", "*.example.com", "**.github.com:443", "crl*.digicert.com:80", "api?.example.com", "api[12].example.com"}

	_, err := ParseGroups(map[string]map[string]any{"g": {"rationale": "x", "allow": allow}})

	if err != nil {
		t.Errorf("ParseGroups() error = %v, want sbx の host pattern を受け入れる", err)
	}
}

func TestExcludingAGroupThatDoesNotExistIsAnError(t *testing.T) {
	_, err := ParseGroups(map[string]map[string]any{"githb": {"enabled": false}})

	if err == nil || !strings.Contains(err.Error(), "egress.githb") {
		t.Errorf("ParseGroups() error = %v, want the misspelled exclusion to be reported", err)
	}
}

func TestExcludingAnExistingGroupIsValid(t *testing.T) {
	_, err := ParseGroups(map[string]map[string]any{"github": {"rationale": "GitHub", "allow": []any{"github.com:443"}, "enabled": false}})

	if err != nil {
		t.Errorf("ParseGroups() error = %v, want enabled: false on top of an existing group to be valid", err)
	}
}

func TestDesiredResourcesSkipDisabledGroupsAndDeduplicate(t *testing.T) {
	disabled := false
	groups := map[string]Group{
		"github": {Rationale: "GitHub", Allow: []string{"github.com:443", "ghcr.io:443"}},
		"mirror": {Rationale: "mirror", Allow: []string{"github.com:443"}},
		"apt":    {Rationale: "apt", Allow: []string{"ports.ubuntu.com:80"}, Enabled: &disabled},
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
	plan, err := Diff(context.Background(), withRules(live), desired)
	if err != nil {
		t.Fatal(err)
	}
	for failOn := 1; failOn <= len(plan.Add)+len(plan.Remove); failOn++ {
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

// --- 同梱の default スコープ ---

func TestEmbeddedDefaultGroupsAreValid(t *testing.T) {
	declared, err := config.LoadGlobalEgress(filepath.Join(t.TempDir(), "no-user-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	groups, err := ParseGroups(declared)

	if err != nil {
		t.Fatalf("ParseGroups(default) error = %v", err)
	}
	want := []string{"cert-validation", "docker-registry", "github", "mise-tools", "ubuntu-apt"}
	if got := slices.Sorted(maps.Keys(groups)); !reflect.DeepEqual(got, want) {
		t.Errorf("default groups = %v, want %v", got, want)
	}
}
