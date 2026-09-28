package sandbox

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// v010Declaration は v0.1.0 が書いた作成時の宣言 (fixture の置き場と作り方は cmd/sbxr/testdata/v0.1.0/README.md)。
const v010Declaration = "../../cmd/sbxr/testdata/v0.1.0/app/declaration.yaml"

func TestTheV010DeclarationDecodesIntoTheCurrentDeclarationWithoutUnknownFields(t *testing.T) {
	data, err := os.ReadFile(v010Declaration)
	if err != nil {
		t.Fatal(err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var declaration sandboxDeclaration
	if err := decoder.Decode(&declaration); err != nil {
		t.Errorf("decode = %v, want every v0.1.0 field kept (fields are never renamed or removed)", err)
	}
}

func TestACreationRecordWithoutTheHerdrFlagIsReportedWithoutACause(t *testing.T) {
	dir := stateDir{path: t.TempDir()}
	writeCreationRecord(t, dir, "{}\n")

	_, err := dir.herdrEnabled(notAskedForTheDefinition(t))

	if err == nil || !strings.Contains(err.Error(), "herdr 連携の有無が無い") || strings.Contains(err.Error(), "<nil>") {
		t.Errorf("herdrEnabled() error = %v, want the missing flag reported without a nil cause", err)
	}
}

func TestAnUnreadableCreationRecordKeepsTheYAMLErrorAsItsCause(t *testing.T) {
	dir := stateDir{path: t.TempDir()}
	writeCreationRecord(t, dir, "herdr: maybe\n")

	_, err := dir.herdrEnabled(notAskedForTheDefinition(t))

	if _, ok := errors.AsType[*yaml.TypeError](err); !ok {
		t.Errorf("herdrEnabled() error = %v, want the YAML error wrapped", err)
	}
}

// notAskedForTheDefinition は、作成の最初の記録があるときは引かれない実行基盤の定義の判定。引かれたら test を止める。
func notAskedForTheDefinition(t *testing.T) func(string) (bool, error) {
	return func(string) (bool, error) {
		t.Fatal("作成の最初の記録があるのに、実行基盤の定義で判定した")
		return false, nil
	}
}

func writeCreationRecord(t *testing.T, dir stateDir, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir.path, creationFile), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}
