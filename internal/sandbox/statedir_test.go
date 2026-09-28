package sandbox

import (
	"bytes"
	"os"
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
