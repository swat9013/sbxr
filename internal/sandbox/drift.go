package sandbox

import (
	"encoding/json"
	"fmt"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/swat9013/sbxr/internal/secret"
)

// creationRecord は状態ディレクトリに残した作成時の記録。宣言と、それを確定したときの repo の egress の扱い
// (git URL を --yes で通して repo の egress を落としたなら、drift を比べるときに同じ扱いを再現する)。
type creationRecord struct {
	Declaration sandboxDeclaration
	RepoEgress  repoEgressPolicy
}

// readRecord は target の作成時の記録を状態ディレクトリから読む。sbx には問い合わせない。
// 作り終えた記録が無い (状態ディレクトリが無い・別の repo のもの・作成が途中で止まった) なら found が false。
func readRecord(places Places, target sandboxTarget) (record creationRecord, found bool, err error) {
	dir := places.stateDirOf(target.Name)
	source, found, err := dir.source()
	if err != nil || !found || source != target.Source() {
		return creationRecord{}, false, err
	}
	return dir.record()
}

// Comparison は作成時の宣言と現在の宣言を比べた結果。
type Comparison struct {
	Differences []Difference
	// NotCompared は作成時に記録していないので比べなかったことの説明。drift ではない。
	NotCompared []string
}

// Difference は作成時の宣言と現在の宣言で値が違う 1 箇所。Path は key を . で繋いだもの (profile.model 等)。
type Difference struct {
	Path     string
	Recorded string
	Current  string
}

// Drift は作成時の宣言と現在の宣言の違いを葉の単位で並べる。宣言の項目は作成時に VM へ焼き込まれるもの (と、配線した secret を
// 突き合わせる名前) だけなので、作成時に記録してある項目は全部比べる (user の egress は宣言に入らず、sbxr policy sync --check が比べる)。
// 両方を同じ YAML の形へ直してから比べるので、書き出し方の違いは差にならない。
// 配線した secret は、配線の結果が揃えた形で比べる (並びを差にしない。作成時に記録していない secret の key・vars は比べず、
// NotCompared に注記する)。
func (r creationRecord) Drift(current sandboxDeclaration) (Comparison, error) {
	var comparison Comparison
	recorded := r.Declaration
	recorded.Secrets, current.Secrets, comparison.NotCompared = secret.Comparable(recorded.Secrets, current.Secrets)
	before, err := declarationTree(recorded)
	if err != nil {
		return Comparison{}, err
	}
	after, err := declarationTree(current)
	if err != nil {
		return Comparison{}, err
	}
	comparison.Differences, err = compareValues("", before, after)
	return comparison, err
}

func declarationTree(decl sandboxDeclaration) (map[string]any, error) {
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
func compareValues(path string, before, after any) ([]Difference, error) {
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
			child := key
			if path != "" {
				child = path + "." + key
			}
			found, err := compareValues(child, beforeMap[key], afterMap[key])
			if err != nil {
				return nil, err
			}
			differences = append(differences, found...)
		}
		return differences, nil
	}
	recorded, err := display(before)
	if err != nil {
		return nil, err
	}
	current, err := display(after)
	if err != nil {
		return nil, err
	}
	if recorded == current {
		return nil, nil
	}
	return []Difference{{Path: path, Recorded: recorded, Current: current}}, nil
}

// display は葉の値を 1 行で見せる。値が無ければ (なし)。
func display(value any) (string, error) {
	if value == nil {
		return "(なし)", nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("宣言の値 %v を比べられない: %w", value, err)
	}
	return string(data), nil
}
