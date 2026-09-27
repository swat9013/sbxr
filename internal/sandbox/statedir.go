package sandbox

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// 状態ディレクトリに sbxr が置くファイル。file の配置と旧い形の読み方は、この file だけが知る。
// 記録は足すだけにし、file と field は改名も削除もしない。新しい記録は、無ければ v0.1.0 の状態ディレクトリとして読む
// (ADR 0006 の改訂。互換は cmd/sbxr/testdata/v0.1.0 の fixture で固定する)。
// 実行基盤の定義 (sbx では env 定義と kit) は Runtime が同じディレクトリに書き、読む。
const (
	// sourceFile は出所 (repo の path か git URL)。状態ディレクトリが sbxr の管理下の印も兼ねる。
	sourceFile = "source"
	// creationFile は作成の最初の記録。v0.1.0 には無い。
	creationFile = "creation.yaml"
	// declarationFile は作成時の宣言。作成がすべて済んでから書き、作成が終わった印にする。
	declarationFile = "declaration.yaml"
	// repoEgressDroppedFile は git URL を --yes で通して repo の egress を落として作った印。
	repoEgressDroppedFile = "repo-egress-dropped"
)

// StateDir は sandbox VM の状態ディレクトリの path。
func (p Places) StateDir(name string) string {
	return filepath.Join(p.StateRoot, name)
}

// creation は作成の最初に書く記録。作成途中の VM の stop と destroy が読む。
type creation struct {
	// Herdr は herdr 連携を有効にして作ったか。file があって値が無ければ、記録が壊れている。
	Herdr *bool `yaml:"herdr"`
}

// stateDir は 1 つの sandbox VM の状態ディレクトリ。
type stateDir struct {
	path string
}

func (p Places) stateDirOf(name string) stateDir {
	return stateDir{path: p.StateDir(name)}
}

// ensure は状態ディレクトリを持ち主だけが読み書きできる mode で作る (VM の環境変数を含む定義が置かれる)。
func (d stateDir) ensure() error {
	if err := os.MkdirAll(d.path, 0o700); err != nil {
		return fmt.Errorf("状態ディレクトリ %s を作れない: %w", d.path, err)
	}
	return nil
}

// writeCreation は作成の最初の記録と出所を書く。実行基盤の定義を書いた後に呼ぶ
// (出所だけが残ると、destroy が定義の無い状態ディレクトリで詰む。ADR 0006)。
// 出所を最後に書くので、出所のある状態ディレクトリには作成の最初の記録もある (v0.1.0 のものを除く)。
func (d stateDir) writeCreation(source string, created creation) error {
	data, err := yaml.Marshal(created)
	if err != nil {
		return err
	}
	if err := d.write(creationFile, data); err != nil {
		return err
	}
	return d.write(sourceFile, []byte(source))
}

// source は記録された出所を返す。状態ディレクトリが無ければ found が false。
func (d stateDir) source() (source string, found bool, err error) {
	data, found, err := d.read(sourceFile)
	return string(data), found, err
}

// writeRecord は作成時の記録を書く。作成時の宣言は作成が終わった印なので最後に書く。
func (d stateDir) writeRecord(record Record) error {
	if record.RepoEgress == DropRepoEgress {
		if err := d.write(repoEgressDroppedFile, nil); err != nil {
			return err
		}
	}
	data, err := yaml.Marshal(record.Declaration)
	if err != nil {
		return err
	}
	return d.write(declarationFile, data)
}

// record は作成時の記録を読む。作成が終わっていなければ found が false。
func (d stateDir) record() (record Record, found bool, err error) {
	data, found, err := d.read(declarationFile)
	if err != nil || !found {
		return Record{}, false, err
	}
	if err := yaml.Unmarshal(data, &record.Declaration); err != nil {
		return Record{}, false, fmt.Errorf("作成時の宣言 %s を読めない: %w", filepath.Join(d.path, declarationFile), err)
	}
	record.RepoEgress = KeepRepoEgress
	if dropped, err := d.exists(repoEgressDroppedFile); err != nil {
		return Record{}, false, err
	} else if dropped {
		record.RepoEgress = DropRepoEgress
	}
	return record, true, nil
}

// herdrEnabled は herdr 連携を有効にして作ったかを、作成の最初の記録から返す。作成途中の VM でも読める。
// 記録の無い v0.1.0 の状態ディレクトリは、実行基盤の定義が herdr を導入するか (definedWithHerdr) で判定する
// (ADR 0007 の改訂)。v0.1.0 が作った状態ディレクトリが残りうる限り、この読み方を残す。
func (d stateDir) herdrEnabled(definedWithHerdr func(stateDir string) (bool, error)) (bool, error) {
	data, found, err := d.read(creationFile)
	if err != nil {
		return false, err
	}
	if !found {
		return definedWithHerdr(d.path)
	}
	var created creation
	if err := yaml.Unmarshal(data, &created); err != nil || created.Herdr == nil {
		return false, fmt.Errorf("作成の最初の記録 %s を読めない (herdr 連携の有無が無い): %v", filepath.Join(d.path, creationFile), err)
	}
	return *created.Herdr, nil
}

// remove は状態ディレクトリを消す。実行基盤の定義も一緒に消えるので、VM を消せた後にだけ呼ぶ。
func (d stateDir) remove() error {
	if err := os.RemoveAll(d.path); err != nil {
		return fmt.Errorf("状態ディレクトリ %s を消せない: %w", d.path, err)
	}
	return nil
}

func (d stateDir) write(file string, data []byte) error {
	if err := os.WriteFile(filepath.Join(d.path, file), data, 0o600); err != nil {
		return fmt.Errorf("状態ディレクトリに %s を書けない: %w", file, err)
	}
	return nil
}

// read はファイルの中身を返す。無ければ found が false。
func (d stateDir) read(file string) (data []byte, found bool, err error) {
	data, err = os.ReadFile(filepath.Join(d.path, file))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("状態ディレクトリ %s の %s を読めない: %w", d.path, file, err)
	}
	return data, true, nil
}

// exists はファイルがあるかを返す。
func (d stateDir) exists(file string) (bool, error) {
	_, err := os.Stat(filepath.Join(d.path, file))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("状態ディレクトリ %s の %s を確かめられない: %w", d.path, file, err)
	}
	return true, nil
}
