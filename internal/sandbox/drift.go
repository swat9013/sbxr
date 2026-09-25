package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"
)

// driftKeys は作成時に VM へ焼き込まれ、drift として比べる宣言の key。
// user の egress (global rule) は sbxr policy sync --check が比べるので入れない。
var driftKeys = []string{"profile", "git", "init", "boot", "sandbox_egress", "secrets", "herdr"}

// Difference は作成時の宣言と現在の宣言で値が違う 1 箇所。Path は key を . で繋いだもの (profile.model 等)。
type Difference struct {
	Path     string
	Recorded string
	Current  string
}

// Drift は既存の sandbox VM について、作成時の宣言と現在の宣言を比べた結果。
type Drift struct {
	// Prepared は現在の宣言に create と同じ確定処理を通した結果 (作成時と同じ repo の egress の扱いで)。
	Prepared    Prepared
	Differences []Difference
}

// CheckDrift は状態ディレクトリの作成時の宣言と、現在の宣言を比べる。
// git URL を --yes で通して作った VM は、現在の宣言からも repo の egress を落として比べる。git URL の Target は呼び出し側が clone してから渡す。
func CheckDrift(ctx context.Context, places Places, target Target) (Drift, error) {
	recorded, err := readDeclaration(places.StateDir(target.Name))
	if err != nil {
		return Drift{}, err
	}
	repoEgress := KeepRepoEgress
	if recorded.RepoEgressDropped {
		repoEgress = DropRepoEgress
	}
	prepared, err := Prepare(ctx, places, target, repoEgress)
	if err != nil {
		return Drift{}, err
	}
	differences, err := compareDeclarations(recorded, prepared.Declaration)
	if err != nil {
		return Drift{}, err
	}
	return Drift{Prepared: prepared, Differences: differences}, nil
}

func readDeclaration(stateDir string) (Declaration, error) {
	path := filepath.Join(stateDir, declarationFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return Declaration{}, fmt.Errorf("作成時の宣言 %s を読めない: %w", path, err)
	}
	var decl Declaration
	if err := yaml.Unmarshal(data, &decl); err != nil {
		return Declaration{}, fmt.Errorf("作成時の宣言 %s を読めない: %w", path, err)
	}
	return decl, nil
}

// compareDeclarations は drift として比べる key について、値が違う箇所を葉の単位で並べる。
// 両方を同じ YAML の形へ直してから比べるので、書き出し方の違い (空の list と null など) は差にならない。
func compareDeclarations(recorded, current Declaration) ([]Difference, error) {
	before, err := declarationTree(recorded)
	if err != nil {
		return nil, err
	}
	after, err := declarationTree(current)
	if err != nil {
		return nil, err
	}
	var differences []Difference
	for _, key := range driftKeys {
		differences = append(differences, compareValues(key, before[key], after[key])...)
	}
	return differences, nil
}

func declarationTree(decl Declaration) (map[string]any, error) {
	data, err := yaml.Marshal(decl)
	if err != nil {
		return nil, err
	}
	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return nil, err
	}
	return tree, nil
}

// compareValues は map を key ごとに降りて比べる。map 以外 (list・scalar) は値全体を 1 つの葉として比べる。
func compareValues(path string, before, after any) []Difference {
	beforeMap, beforeIsMap := before.(map[string]any)
	afterMap, afterIsMap := after.(map[string]any)
	if beforeIsMap && afterIsMap {
		var keys []string
		for key := range beforeMap {
			keys = append(keys, key)
		}
		for key := range afterMap {
			if _, ok := beforeMap[key]; !ok {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		var differences []Difference
		for _, key := range keys {
			differences = append(differences, compareValues(path+"."+key, beforeMap[key], afterMap[key])...)
		}
		return differences
	}
	recorded, current := display(before), display(after)
	if recorded == current {
		return nil
	}
	return []Difference{{Path: path, Recorded: recorded, Current: current}}
}

// display は葉の値を 1 行で見せる。値が無ければ (なし)。
func display(value any) string {
	if value == nil {
		return "(なし)"
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}
