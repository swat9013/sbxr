package sandbox

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/config"
	"github.com/swat9013/sbxr/internal/secret"
)

func TestDriftListsEachChangedLeafOfTheDeclaration(t *testing.T) {
	base := func() Declaration {
		return Declaration{
			Profile: config.Profile{Env: map[string]string{"A": "1"}},
			Init:    []string{"make"},
			Secrets: []secret.Recorded{{Name: "one", Key: "ONE", Hosts: []string{"a.example.com", "b.example.com"}, Env: "ONE"}, {Name: "two", Key: "TWO", Hosts: []string{"c.example.com"}, Env: "TWO"}},
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
			d.Secrets = []secret.Recorded{{Name: "two", Key: "TWO", Hosts: []string{"c.example.com"}, Env: "TWO"}, {Name: "one", Key: "ONE", Hosts: []string{"b.example.com", "a.example.com"}, Env: "ONE"}}
		}, nil},
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

func TestDriftOfARecordWithoutSecretKeysNotesWhatItDidNotCompare(t *testing.T) {
	recorded := Declaration{Secrets: []secret.Recorded{{Name: "gitlab", Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN"}}}
	current := Declaration{Secrets: []secret.Recorded{{Name: "gitlab", Key: "GITLAB_TOKEN", Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Vars: map[string]string{"GITLAB_HOST": "gitlab.example.com"}}}}

	got, err := Record{Declaration: recorded}.Drift(current)

	if err != nil || got.Differences != nil || len(got.NotCompared) != 1 || !strings.Contains(got.NotCompared[0], "gitlab") {
		t.Errorf("Drift = %+v, %v, want no difference and a note for gitlab", got, err)
	}
}

func TestTheDeclarationHoldsOnlyTheKeysBakedIntoTheVM(t *testing.T) {
	tree, err := declarationTree(Declaration{
		Herdr:   &HerdrPin{},
		Secrets: []secret.Recorded{{Service: "github", Key: "K", Env: "E", Vars: map[string]string{"V": "1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	keys := slices.Sorted(maps.Keys(tree))
	secretKeys := slices.Sorted(maps.Keys(tree["secrets"].([]any)[0].(map[string]any)))

	// Drift は宣言の key を全部比べる。焼き込まれない key を足すなら、比べる key を絞り直す
	want := []string{"boot", "git", "herdr", "init", "profile", "sandbox_egress", "secrets"}
	if !slices.Equal(keys, want) {
		t.Errorf("declaration keys = %v, want %v", keys, want)
	}
	// secret は値を除いて焼き込まれる (service・hosts・env は実行基盤の secret、key は置く値、vars は VM の環境変数)
	wantSecret := []string{"env", "hosts", "key", "name", "service", "vars"}
	if !slices.Equal(secretKeys, wantSecret) {
		t.Errorf("secret keys = %v, want %v", secretKeys, wantSecret)
	}
}
