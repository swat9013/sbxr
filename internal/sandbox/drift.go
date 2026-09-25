package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// driftKeys は作成時に VM へ焼き込まれ、drift として比べる宣言の key。
// user の egress (global rule) は sbxr policy sync --check が比べるので入れない。
var driftKeys = []string{"profile", "git", "init", "boot", "sandbox_egress", "secrets", "herdr"}

// Record は状態ディレクトリに残した作成時の記録。宣言と、それを確定したときの repo の egress の扱い。
type Record struct {
	Declaration Declaration
	RepoEgress  RepoEgressPolicy
}

// recordFile は declaration.yaml の形。宣言の key に、git URL を --yes で通して repo の egress を落としたかを足す。
type recordFile struct {
	Declaration       `yaml:",inline"`
	RepoEgressDropped bool `yaml:"repo_egress_dropped,omitempty"`
}

func writeRecord(dir string, record Record) error {
	data, err := yaml.Marshal(recordFile{Declaration: record.Declaration, RepoEgressDropped: record.RepoEgress == DropRepoEgress})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, declarationFile), data, 0o600); err != nil {
		return fmt.Errorf("状態ディレクトリに %s を書けない: %w", declarationFile, err)
	}
	return nil
}

// ReadRecord は target の作成時の記録を状態ディレクトリから読む。sbx には問い合わせない。
// 作り終えた記録が無い (状態ディレクトリが無い・別の repo のもの・作成が途中で止まった) なら found が false。
func ReadRecord(places Places, target Target) (record Record, found bool, err error) {
	dir := places.StateDir(target.Name)
	source, err := os.ReadFile(filepath.Join(dir, sourceFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("状態ディレクトリ %s を読めない: %w", dir, err)
	}
	if string(source) != target.Source() {
		return Record{}, false, nil
	}
	path := filepath.Join(dir, declarationFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("作成時の宣言 %s を読めない: %w", path, err)
	}
	var file recordFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return Record{}, false, fmt.Errorf("作成時の宣言 %s を読めない: %w", path, err)
	}
	record = Record{Declaration: file.Declaration, RepoEgress: KeepRepoEgress}
	if file.RepoEgressDropped {
		record.RepoEgress = DropRepoEgress
	}
	return record, true, nil
}

// Difference は作成時の宣言と現在の宣言で値が違う 1 箇所。Path は key を . で繋いだもの (profile.model 等)。
type Difference struct {
	Path     string
	Recorded string
	Current  string
}

// Drift は作成時の宣言と、現在の宣言 (作成時と同じ repo の egress の扱いで確定したもの) の違いを葉の単位で並べる。
// 両方を同じ YAML の形へ直してから比べるので、書き出し方の違いは差にならない。
// secret の並びと注入先 host の並びは VM に効かないので、並べ替えてから比べる。
func (r Record) Drift(current Declaration) ([]Difference, error) {
	before, err := declarationTree(r.Declaration)
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
	decl.Secrets = slices.Clone(decl.Secrets)
	for i := range decl.Secrets {
		decl.Secrets[i].Hosts = slices.Sorted(slices.Values(decl.Secrets[i].Hosts))
	}
	slices.SortFunc(decl.Secrets, func(a, b WiredSecret) int { return strings.Compare(a.Name, b.Name) })
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
