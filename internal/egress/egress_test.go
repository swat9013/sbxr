package egress

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
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

// --- 差分 (旧 reconcile のケースを移植) ---

func TestDiff(t *testing.T) {
	tests := []struct {
		name       string
		live       []sbxstub.Rule
		desired    []string
		wantRemove []string
		wantAdd    []string
	}{
		{
			name:    "宣言と一致する単一 resource の rule は残す",
			live:    []sbxstub.Rule{sbxstub.GlobalAllow("r1", "github.com:443")},
			desired: []string{"github.com:443"},
		},
		{
			name:       "宣言に無い rule は消して、足りない宛先を足す",
			live:       []sbxstub.Rule{sbxstub.GlobalAllow("r1", "evil.example.com:443")},
			desired:    []string{"github.com:443"},
			wantRemove: []string{"r1"},
			wantAdd:    []string{"github.com:443"},
		},
		{
			name:       "複数 resource の rule は宣言と一致していても 1 resource 単位へ置き換える",
			live:       []sbxstub.Rule{sbxstub.GlobalAllow("r1", "a.example.com:443", "b.example.com:443")},
			desired:    []string{"a.example.com:443", "b.example.com:443"},
			wantRemove: []string{"r1"},
			wantAdd:    []string{"a.example.com:443", "b.example.com:443"},
		},
		{
			name:       "同じ宛先を担う重複 rule は後の方を消す",
			live:       []sbxstub.Rule{sbxstub.GlobalAllow("r1", "github.com:443"), sbxstub.GlobalAllow("r2", "github.com:443")},
			desired:    []string{"github.com:443"},
			wantRemove: []string{"r2"},
		},
		{
			name: "手で足した global の deny rule も宣言に無いので消す",
			live: []sbxstub.Rule{
				{ID: "d1", Scope: "global", ResourceType: "network", Decision: "deny", Resources: []string{"github.com:443"}, Editable: true},
			},
			desired:    []string{"github.com:443"},
			wantRemove: []string{"d1"},
			wantAdd:    []string{"github.com:443"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &sbxstub.Stub{Rules: tt.live}

			plan, err := Diff(context.Background(), runtime.NewSbx(stub.Run), tt.desired)

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
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{sbxstub.GlobalAllow("r1", "evil.example.com:443")}}

	_, err := Diff(context.Background(), runtime.NewSbx(stub.Run), []string{"github.com:443"})

	if err != nil || len(stub.Writes) != 0 {
		t.Errorf("Diff() error = %v, sbx writes = %q, want no writes", err, stub.Writes)
	}
}

// --- 収束 ---

func TestConvergeTwiceMakesNoChangesTheSecondTime(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{
		sbxstub.GlobalAllow("r1", "evil.example.com:443"),
		sbxstub.GlobalAllow("r2", "a.example.com:443", "github.com:443"),
	}}
	sbx := runtime.NewSbx(stub.Run)
	desired := []string{"a.example.com:443", "github.com:443"}

	if _, err := Converge(context.Background(), sbx, desired); err != nil {
		t.Fatalf("1 回目の Converge() error = %v", err)
	}
	second, err := Converge(context.Background(), sbx, desired)

	if err != nil {
		t.Fatalf("2 回目の Converge() error = %v", err)
	}
	if !second.Empty() {
		t.Errorf("2 回目の Converge() = %+v, want no changes", second)
	}
}

func TestConvergeLeavesRulesOutsideItsTargetAlone(t *testing.T) {
	scoped := sbxstub.Rule{ID: "scoped", Scope: "sandbox:vm", ResourceType: "network", Decision: "allow", Resources: []string{"api.anthropic.com:443"}}
	fixed := sbxstub.Rule{ID: "fixed", Scope: "global", ResourceType: "network", Decision: "allow", Resources: []string{"x.example.com:443"}}
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{scoped, fixed}}

	_, err := Converge(context.Background(), runtime.NewSbx(stub.Run), []string{"github.com:443"})

	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if want := []string{"policy allow network github.com:443"}; !reflect.DeepEqual(stub.Writes, want) {
		t.Errorf("sbx writes = %q, want only the missing allow (sandbox スコープと editable でない rule には触れない)", stub.Writes)
	}
}

func ruleIDs(rules []runtime.EgressRule) []string {
	var ids []string
	for _, r := range rules {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestConvergeAddsBeforeRemovingSoDeclaredHostsStayReachable(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{sbxstub.GlobalAllow("r1", "github.com:443", "ghcr.io:443")}}

	_, err := Converge(context.Background(), runtime.NewSbx(stub.Run), []string{"ghcr.io:443", "github.com:443"})

	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	want := []string{"policy allow network ghcr.io:443", "policy allow network github.com:443", "policy rm network --id r1"}
	if !reflect.DeepEqual(stub.Writes, want) {
		t.Errorf("sbx writes = %q, want %q", stub.Writes, want)
	}
}

func TestConvergeFailsWhenTheReadBackStillDiffers(t *testing.T) {
	stub := &sbxstub.Stub{DropWrites: true}

	_, err := Converge(context.Background(), runtime.NewSbx(stub.Run), []string{"github.com:443"})

	if err == nil || !strings.Contains(err.Error(), "適用後") {
		t.Errorf("Converge() error = %v, want a read-back failure", err)
	}
}

// --- 同梱の default スコープ ---

func TestEmbeddedDefaultGroupsAreValid(t *testing.T) {
	dir := t.TempDir()
	userPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(userPath, []byte("version: 1\ngit:\n  name: probe\n  email: probe@example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(userPath, filepath.Join(dir, "sbxr.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	groups, err := ParseGroups(cfg.GlobalEgress)

	if err != nil {
		t.Fatalf("ParseGroups(default) error = %v", err)
	}
	want := []string{"cert-validation", "docker-registry", "github", "mise-tools", "ubuntu-apt"}
	if got := slices.Sorted(maps.Keys(groups)); !reflect.DeepEqual(got, want) {
		t.Errorf("default groups = %v, want %v", got, want)
	}
}
