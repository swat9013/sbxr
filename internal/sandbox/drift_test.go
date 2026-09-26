package sandbox

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/swat9013/sbxr/internal/config"
)

func TestDriftListsEachChangedLeafOfTheDeclaration(t *testing.T) {
	base := func() Declaration {
		return Declaration{
			Profile: config.Profile{Env: map[string]string{"A": "1"}},
			Init:    []string{"make"},
			Secrets: []WiredSecret{{Name: "one", Hosts: []string{"a.example.com", "b.example.com"}, Env: "ONE"}, {Name: "two", Hosts: []string{"c.example.com"}, Env: "TWO"}},
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*Declaration)
		want   []Difference
	}{
		{"nothing changed", func(*Declaration) {}, nil},
		{"a nested value", func(d *Declaration) { d.Profile.Env = map[string]string{"A": "2"} },
			[]Difference{{Path: "profile.env.A", Recorded: `"1"`, Current: `"2"`}}},
		{"a value that was not set", func(d *Declaration) { level := "high"; d.Profile.EffortLevel = &level },
			[]Difference{{Path: "profile.effortLevel", Recorded: "(なし)", Current: `"high"`}}},
		{"a list", func(d *Declaration) { d.Init = []string{"make", "make test"} },
			[]Difference{{Path: "init", Recorded: `["make"]`, Current: `["make","make test"]`}}},
		{"herdr turned on", func(d *Declaration) { d.Herdr = &HerdrPin{Version: "v0.9.0"} },
			[]Difference{{Path: "herdr", Recorded: "(なし)", Current: `{"version":"v0.9.0"}`}}},
		{"secrets and hosts in another order", func(d *Declaration) {
			d.Secrets = []WiredSecret{{Name: "two", Hosts: []string{"c.example.com"}, Env: "TWO"}, {Name: "one", Hosts: []string{"b.example.com", "a.example.com"}, Env: "ONE"}}
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := base()
			tc.change(&current)

			got, err := Record{Declaration: base()}.Drift(current)

			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Drift = %v, %v, want %v", got, err, tc.want)
			}
		})
	}
}

func TestTheDeclarationHoldsOnlyTheKeysBakedIntoTheVM(t *testing.T) {
	tree, err := declarationTree(Declaration{Herdr: &HerdrPin{}})
	if err != nil {
		t.Fatal(err)
	}

	keys := slices.Sorted(maps.Keys(tree))

	// Drift は宣言の key を全部比べる。焼き込まれない key を足すなら、比べる key を絞り直す
	want := []string{"boot", "git", "herdr", "init", "profile", "sandbox_egress", "secrets"}
	if !slices.Equal(keys, want) {
		t.Errorf("declaration keys = %v, want %v", keys, want)
	}
}
