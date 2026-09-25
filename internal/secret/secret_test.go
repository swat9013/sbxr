package secret

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

func assertErrorMentions(t *testing.T, err error, needles ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want an error mentioning %q", needles)
	}
	for _, needle := range needles {
		if !strings.Contains(err.Error(), needle) {
			t.Errorf("error = %q, want it to mention %q", err, needle)
		}
	}
}

func writeSecretFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secrets.env")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // umask に左右されないよう明示する
		t.Fatal(err)
	}
	return path
}

// --- secret ファイル ---

func TestReadFileRejectsAFileOthersCanRead(t *testing.T) {
	path := writeSecretFile(t, "GITHUB_TOKEN=x\n", 0o644)

	_, err := ReadFile(path)

	assertErrorMentions(t, err, "0600", path)
}

func TestReadFileParsesDotenvLinesSkippingCommentsAndBlanks(t *testing.T) {
	path := writeSecretFile(t, "# comment\n\nGITHUB_TOKEN=ghp_abc\nQUOTED=\"a b\"\nSINGLE='c=d'\n", 0o600)

	values, err := ReadFile(path)

	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	want := Values{"GITHUB_TOKEN": "ghp_abc", "QUOTED": "a b", "SINGLE": "c=d"}
	if !reflect.DeepEqual(values, want) {
		t.Errorf("ReadFile() = %v, want %v", values, want)
	}
}

func TestReadFileRejectsALineThatIsNotKeyEqualsValue(t *testing.T) {
	path := writeSecretFile(t, "GITHUB_TOKEN ghp_abc\n", 0o600)

	_, err := ReadFile(path)

	assertErrorMentions(t, err, "1 行目")
}

func TestReadFileTreatsAMissingFileAsNoValues(t *testing.T) {
	values, err := ReadFile(filepath.Join(t.TempDir(), "secrets.env"))

	if err != nil || len(values) != 0 {
		t.Errorf("ReadFile(missing) = %v, %v, want no values and no error", values, err)
	}
}

func TestWriteValueCreatesTheFileWithMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sbxr", "secrets.env")

	if err := WriteValue(path, "GITHUB_TOKEN", "ghp_abc"); err != nil {
		t.Fatalf("WriteValue() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 600", info.Mode().Perm())
	}
	values, err := ReadFile(path)
	if err != nil || values["GITHUB_TOKEN"] != "ghp_abc" {
		t.Errorf("ReadFile() = %v, %v, want the written token", values, err)
	}
}

func TestWriteValueReplacesTheKeyAndKeepsOtherLines(t *testing.T) {
	path := writeSecretFile(t, "# mine\nOTHER=1\nGITHUB_TOKEN=old\n", 0o600)

	if err := WriteValue(path, "GITHUB_TOKEN", "new"); err != nil {
		t.Fatalf("WriteValue() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := "# mine\nOTHER=1\nGITHUB_TOKEN=new\n"; string(data) != want {
		t.Errorf("file = %q, want %q", data, want)
	}
}

func TestWriteValueRejectsAValueThatWouldBreakTheLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")

	err := WriteValue(path, "GITHUB_TOKEN", "a\nB=evil")

	assertErrorMentions(t, err, "改行")
	if _, statErr := os.Stat(path); statErr == nil {
		t.Errorf("secret file was written despite the error")
	}
}

// --- secret 定義 ---

func TestParseDefinitionsReadsServiceAndPlaceholderDefinitions(t *testing.T) {
	defs, err := ParseDefinitions(map[string]map[string]any{
		"github": {"service": "github", "key": "GITHUB_TOKEN", "hosts": []any{"github.com"}},
		"gitlab": {"key": "GITLAB_TOKEN", "hosts": []any{"gitlab.example.com"}, "env": "GITLAB_TOKEN", "vars": map[string]any{"GITLAB_HOST": "gitlab.example.com"}},
	})

	if err != nil {
		t.Fatalf("ParseDefinitions() error = %v", err)
	}
	want := map[string]Definition{
		"github": {Service: "github", Key: "GITHUB_TOKEN", Hosts: []string{"github.com"}},
		"gitlab": {Key: "GITLAB_TOKEN", Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Vars: map[string]string{"GITLAB_HOST": "gitlab.example.com"}},
	}
	if !reflect.DeepEqual(defs, want) {
		t.Errorf("ParseDefinitions() = %+v, want %+v", defs, want)
	}
}

func TestParseDefinitionsRejectsIncompleteOrAmbiguousDefinitions(t *testing.T) {
	for name, tc := range map[string]struct {
		raw     map[string]any
		mention string
	}{
		"key が無い":                 {map[string]any{"hosts": []any{"a.example.com"}, "env": "A"}, "key"},
		"hosts が無い":               {map[string]any{"key": "A", "env": "A"}, "hosts"},
		"placeholder 注入に env が無い": {map[string]any{"key": "A", "hosts": []any{"a.example.com"}}, "env"},
		"service と env を両方書く":     {map[string]any{"service": "github", "key": "A", "hosts": []any{"github.com"}, "env": "A"}, "env"},
		"host に glob":             {map[string]any{"key": "A", "hosts": []any{"*.example.com"}, "env": "A"}, "*.example.com"},
		"host に port":             {map[string]any{"key": "A", "hosts": []any{"a.example.com:443"}, "env": "A"}, "a.example.com:443"},
		"未知の field":               {map[string]any{"key": "A", "hosts": []any{"a.example.com"}, "env": "A", "value": "leak"}, "value"},
		"env が環境変数名でない":           {map[string]any{"key": "A", "hosts": []any{"a.example.com"}, "env": "A-B"}, "A-B"},
		"key が secret ファイルのキーでない": {map[string]any{"key": "A B", "hosts": []any{"a.example.com"}, "env": "A"}, "A B"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDefinitions(map[string]map[string]any{"broken": tc.raw})

			assertErrorMentions(t, err, "secret_defs.broken", tc.mention)
		})
	}
}

// --- 配線の計画 ---

var testDefs = map[string]Definition{
	"github": {Service: "github", Key: "GITHUB_TOKEN", Hosts: []string{"github.com", "api.github.com"}},
	"gitlab": {Key: "GITLAB_TOKEN", Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN"},
}

var githubEgress = []string{"github.com:443", "**.github.com:443"}

func TestPlanWiresOnlyRequestedSecrets(t *testing.T) {
	plan, err := PlanWiring([]string{"github"}, testDefs, append(githubEgress, "gitlab.example.com:443"))

	if err != nil {
		t.Fatalf("PlanWiring() error = %v", err)
	}
	if want := []Wire{{Name: "github", Definition: testDefs["github"]}}; !reflect.DeepEqual(plan.Wired, want) {
		t.Errorf("wired = %+v, want only the requested github", plan.Wired)
	}
}

func TestPlanDoesNotWireASecretWhoseHostIsNotAllowedByEgress(t *testing.T) {
	plan, err := PlanWiring([]string{"github", "gitlab"}, testDefs, githubEgress)

	if err != nil {
		t.Fatalf("PlanWiring() error = %v", err)
	}
	if len(plan.Wired) != 1 || plan.Wired[0].Name != "github" {
		t.Errorf("wired = %+v, want only github", plan.Wired)
	}
	if want := []Skip{{Name: "gitlab", DeniedHosts: []string{"gitlab.example.com"}}}; !reflect.DeepEqual(plan.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", plan.Skipped, want)
	}
}

func TestPlanDoesNotWireASecretWhenOnlySomeOfItsHostsAreAllowed(t *testing.T) {
	plan, err := PlanWiring([]string{"github"}, testDefs, []string{"github.com:443"})

	if err != nil {
		t.Fatalf("PlanWiring() error = %v", err)
	}
	if len(plan.Wired) != 0 {
		t.Errorf("wired = %+v, want none while api.github.com is not allowed", plan.Wired)
	}
	if want := []Skip{{Name: "github", DeniedHosts: []string{"api.github.com"}}}; !reflect.DeepEqual(plan.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", plan.Skipped, want)
	}
}

func TestPlanStopsOnRequestsWithoutADefinitionAndNamesThemAll(t *testing.T) {
	_, err := PlanWiring([]string{"github", "jira", "aws"}, testDefs, githubEgress)

	assertErrorMentions(t, err, "aws", "jira")
}

// --- 配線の適用 ---

func TestApplyWiresSandboxScopedSecretsPassingValuesOnStdin(t *testing.T) {
	stub := &sbxstub.Stub{}
	plan := Plan{Wired: []Wire{{Name: "github", Definition: testDefs["github"]}, {Name: "gitlab", Definition: testDefs["gitlab"]}}}

	err := Apply(context.Background(), runtime.NewSbx(stub.Run), "vm1", plan, Values{"GITHUB_TOKEN": "gh-value", "GITLAB_TOKEN": "gl-value"})

	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	want := []string{
		"secret set github --sandbox vm1",
		"secret set-custom --sandbox vm1 --host gitlab.example.com --env GITLAB_TOKEN",
	}
	if !reflect.DeepEqual(stub.Writes, want) {
		t.Errorf("sbx writes = %q, want %q", stub.Writes, want)
	}
	if wantInputs := []string{"gh-value\n", "gl-value\n"}; !reflect.DeepEqual(stub.Inputs, wantInputs) {
		t.Errorf("sbx stdin = %q, want the values on stdin", stub.Inputs)
	}
}

func TestApplyWritesNothingWhenAWiredValueIsMissingFromTheSecretFile(t *testing.T) {
	stub := &sbxstub.Stub{}
	plan := Plan{Wired: []Wire{{Name: "github", Definition: testDefs["github"]}, {Name: "gitlab", Definition: testDefs["gitlab"]}}}

	err := Apply(context.Background(), runtime.NewSbx(stub.Run), "vm1", plan, Values{"GITHUB_TOKEN": "gh-value"})

	assertErrorMentions(t, err, "GITLAB_TOKEN")
	if len(stub.Writes) != 0 {
		t.Errorf("sbx writes = %q, want none before every value is found", stub.Writes)
	}
}
