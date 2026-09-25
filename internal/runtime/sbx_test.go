package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

func TestSbxListsOnlyEditableGlobalNetworkRules(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{
		sbxstub.GlobalAllow("global-1", "github.com:443"),
		{ID: "scoped", Scope: "sandbox:vm", ResourceType: "network", Decision: "allow", Resources: []string{"api.anthropic.com:443"}},
		{ID: "fs", Scope: "global", ResourceType: "filesystem:read", Decision: "allow", Resources: []string{"**"}},
		{ID: "fixed", Scope: "global", ResourceType: "network", Decision: "allow", Resources: []string{"x.example.com:443"}, Editable: false},
		{ID: "global-deny", Scope: "global", ResourceType: "network", Decision: "deny", Resources: []string{"evil.example.com:443"}, Editable: true},
	}}

	rules, err := NewSbx(stub.Run).ListGlobalEgressRules(context.Background())

	if err != nil {
		t.Fatalf("ListGlobalEgressRules() error = %v", err)
	}
	want := []EgressRule{
		{ID: "global-1", Decision: "allow", Resources: []string{"github.com:443"}},
		{ID: "global-deny", Decision: "deny", Resources: []string{"evil.example.com:443"}},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Errorf("ListGlobalEgressRules() = %+v, want %+v", rules, want)
	}
}

func TestSbxAllowsOneResourcePerCommand(t *testing.T) {
	stub := &sbxstub.Stub{}

	err := NewSbx(stub.Run).AllowGlobalEgress(context.Background(), "github.com:443")

	if err != nil {
		t.Fatalf("AllowGlobalEgress() error = %v", err)
	}
	if want := []string{"policy allow network github.com:443"}; !reflect.DeepEqual(stub.Writes, want) {
		t.Errorf("sbx writes = %q, want %q", stub.Writes, want)
	}
}

func TestSbxRemovesARuleByID(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{sbxstub.GlobalAllow("r1", "evil.example.com:443")}}

	err := NewSbx(stub.Run).RemoveGlobalEgressRule(context.Background(), "r1")

	if err != nil {
		t.Fatalf("RemoveGlobalEgressRule() error = %v", err)
	}
	if want := []string{"policy rm network --id r1"}; !reflect.DeepEqual(stub.Writes, want) {
		t.Errorf("sbx writes = %q, want %q", stub.Writes, want)
	}
}

func TestSbxRejectsARuleWithAnUnknownDecision(t *testing.T) {
	stub := &sbxstub.Stub{Rules: []sbxstub.Rule{
		{ID: "r1", Scope: "global", ResourceType: "network", Decision: "audit", Resources: []string{"x.example.com:443"}, Editable: true},
	}}

	_, err := NewSbx(stub.Run).ListGlobalEgressRules(context.Background())

	if err == nil {
		t.Errorf("ListGlobalEgressRules() error = nil, want an error for the decision audit")
	}
}
