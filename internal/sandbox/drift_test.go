package sandbox

import (
	"maps"
	"reflect"
	"slices"
	"testing"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/secret"
)

func TestDriftListsEachChangedLeafOfTheDeclaration(t *testing.T) {
	base := func() Declaration {
		return Declaration{
			Profile: config.Profile{Env: map[string]string{"A": "1"}},
			Init:    []string{"make"},
			Secrets: []secret.WiredSecret{{Name: "one", Key: "ONE", Hosts: []string{"a.example.com", "b.example.com"}, Env: "ONE"}, {Name: "two", Key: "TWO", Hosts: []string{"c.example.com"}, Env: "TWO"}},
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
			d.Secrets = []secret.WiredSecret{{Name: "two", Key: "TWO", Hosts: []string{"c.example.com"}, Env: "TWO"}, {Name: "one", Key: "ONE", Hosts: []string{"b.example.com", "a.example.com"}, Env: "ONE"}}
		}, nil},
		{"the vars of a secret", func(d *Declaration) { d.Secrets[0].Vars = map[string]string{"HOST": "a.example.com"} },
			[]Difference{{Path: "secrets",
				Recorded: `[{"env":"ONE","hosts":["a.example.com","b.example.com"],"key":"ONE","name":"one"},{"env":"TWO","hosts":["c.example.com"],"key":"TWO","name":"two"}]`,
				Current:  `[{"env":"ONE","hosts":["a.example.com","b.example.com"],"key":"ONE","name":"one","vars":{"HOST":"a.example.com"}},{"env":"TWO","hosts":["c.example.com"],"key":"TWO","name":"two"}]`}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := base()
			tc.change(&current)

			got, err := Record{Declaration: base()}.Drift(current)

			if err != nil || !reflect.DeepEqual(got.Differences, tc.want) {
				t.Errorf("Drift = %v, %v, want %v", got.Differences, err, tc.want)
			}
		})
	}
}

func TestTheDeclarationHoldsOnlyTheKeysBakedIntoTheVM(t *testing.T) {
	tree, err := declarationTree(Declaration{
		Herdr:   &HerdrPin{},
		Secrets: []secret.WiredSecret{{Service: "github", Key: "K", Env: "E", Vars: map[string]string{"V": "1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	keys := slices.Sorted(maps.Keys(tree))
	secretFields := tree["secrets"].([]any)[0].(map[string]any)
	_, named := secretFields["name"]
	delete(secretFields, "name") // name は焼き込まれず、作成時と現在の記録を突き合わせる識別子
	secretKeys := slices.Sorted(maps.Keys(secretFields))

	// Drift は宣言の key を全部比べる。焼き込まれない key を足すなら、比べる key を絞り直す
	want := []string{"boot", "git", "herdr", "init", "profile", "sandbox_egress", "secrets"}
	if !slices.Equal(keys, want) {
		t.Errorf("declaration keys = %v, want %v", keys, want)
	}
	// secret は値を除いて焼き込まれる (service・hosts・env は実行基盤の secret、key は置く値、vars は VM の環境変数)
	wantSecret := []string{"env", "hosts", "key", "service", "vars"}
	if !named || !slices.Equal(secretKeys, wantSecret) {
		t.Errorf("secret keys = %v (name: %v), want %v and the name", secretKeys, named, wantSecret)
	}
}
