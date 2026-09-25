package sandbox

import (
	"context"
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

// repoEgressDroppedFile は git URL を --yes で通して repo の egress を落として作った印。drift を比べるときに同じ扱いを再現する。
const repoEgressDroppedFile = "repo-egress-dropped"

// Record は状態ディレクトリに残した作成時の記録。宣言と、それを確定したときの repo の egress の扱い。
type Record struct {
	Declaration Declaration
	RepoEgress  RepoEgressPolicy
}

// writeRecord は作成時の記録を書く。declaration.yaml は作成が終わった印なので最後に書く。
func writeRecord(dir string, record Record) error {
	if record.RepoEgress == DropRepoEgress {
		if err := os.WriteFile(filepath.Join(dir, repoEgressDroppedFile), nil, 0o600); err != nil {
			return fmt.Errorf("状態ディレクトリに %s を書けない: %w", repoEgressDroppedFile, err)
		}
	}
	data, err := yaml.Marshal(record.Declaration)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, declarationFile), data, 0o600); err != nil {
		return fmt.Errorf("状態ディレクトリに %s を書けない: %w", declarationFile, err)
	}
	return nil
}

// recordedSource は状態ディレクトリに記録された出所を返す。状態ディレクトリが無ければ found が false。
func recordedSource(dir string) (source string, found bool, err error) {
	data, err := os.ReadFile(filepath.Join(dir, sourceFile))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("状態ディレクトリ %s を読めない: %w", dir, err)
	}
	return string(data), true, nil
}

// ReadRecord は target の作成時の記録を状態ディレクトリから読む。sbx には問い合わせない。
// 作り終えた記録が無い (状態ディレクトリが無い・別の repo のもの・作成が途中で止まった) なら found が false。
func ReadRecord(places Places, target Target) (record Record, found bool, err error) {
	dir := places.StateDir(target.Name)
	source, found, err := recordedSource(dir)
	if err != nil || !found || source != target.Source() {
		return Record{}, false, err
	}
	path := filepath.Join(dir, declarationFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("作成時の宣言 %s を読めない: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &record.Declaration); err != nil {
		return Record{}, false, fmt.Errorf("作成時の宣言 %s を読めない: %w", path, err)
	}
	record.RepoEgress = KeepRepoEgress
	if _, err := os.Stat(filepath.Join(dir, repoEgressDroppedFile)); err == nil {
		record.RepoEgress = DropRepoEgress
	} else if !errors.Is(err, fs.ErrNotExist) {
		return Record{}, false, err
	}
	return record, true, nil
}

// CompareWithRecord は現在の宣言を作成時と同じ repo の egress の扱いで確定し、作成時の宣言との差分を返す。
// git URL の Target は呼び出し側が clone してから渡す。
func CompareWithRecord(ctx context.Context, places Places, target Target, record Record) ([]Difference, Prepared, error) {
	prepared, err := Prepare(ctx, places, target, record.RepoEgress)
	if err != nil {
		return nil, Prepared{}, err
	}
	differences, err := record.Drift(prepared.Declaration)
	return differences, prepared, err
}

// Difference は作成時の宣言と現在の宣言で値が違う 1 箇所。Path は key を . で繋いだもの (profile.model 等)。
type Difference struct {
	Path     string
	Recorded string
	Current  string
}

// Drift は作成時の宣言と現在の宣言の違いを葉の単位で並べる。宣言の key はすべて作成時に VM へ焼き込まれるものなので、全部比べる
// (user の egress は宣言に入らず、sbxr policy sync --check が比べる)。
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
	return compareValues("", before, after)
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
