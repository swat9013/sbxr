package secret

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/runtime"
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

func TestValidateAcceptsServiceAndPlaceholderDefinitions(t *testing.T) {
	for name, def := range map[string]Definition{
		"service":     {Service: "github", Key: "GITHUB_TOKEN", Hosts: []string{"github.com"}},
		"placeholder": {Key: "GITLAB_TOKEN", Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Vars: map[string]string{"GITLAB_HOST": "gitlab.example.com"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := def.Validate(); err != nil {
				t.Errorf("Validate() error = %v, want the definition accepted", err)
			}
		})
	}
}

func TestValidateRejectsIncompleteOrAmbiguousDefinitions(t *testing.T) {
	for name, tc := range map[string]struct {
		def     Definition
		mention string
	}{
		"key が無い":                 {Definition{Hosts: []string{"a.example.com"}, Env: "A"}, "key"},
		"hosts が無い":               {Definition{Key: "A", Env: "A"}, "hosts"},
		"placeholder 注入に env が無い": {Definition{Key: "A", Hosts: []string{"a.example.com"}}, "env"},
		"service と env を両方書く":     {Definition{Service: "github", Key: "A", Hosts: []string{"github.com"}, Env: "A"}, "env"},
		"host に glob":             {Definition{Key: "A", Hosts: []string{"*.example.com"}, Env: "A"}, "*.example.com"},
		"host に port":             {Definition{Key: "A", Hosts: []string{"a.example.com:443"}, Env: "A"}, "a.example.com:443"},
		"env が環境変数名でない":           {Definition{Key: "A", Hosts: []string{"a.example.com"}, Env: "A-B"}, "A-B"},
		"key が secret ファイルのキーでない": {Definition{Key: "A B", Hosts: []string{"a.example.com"}, Env: "A"}, "A B"},
	} {
		t.Run(name, func(t *testing.T) {
			assertErrorMentions(t, tc.def.Validate(), tc.mention)
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

func TestWriteValueKeepsLinesAfterAVeryLongValue(t *testing.T) {
	long := strings.Repeat("x", 100_000)
	path := writeSecretFile(t, "LONG="+long+"\nAFTER=1\n", 0o600)

	if err := WriteValue(path, "NEW", "v"); err != nil {
		t.Fatalf("WriteValue() error = %v", err)
	}

	values, err := ReadFile(path)
	if err != nil || values["LONG"] != long || values["AFTER"] != "1" || values["NEW"] != "v" {
		t.Errorf("ReadFile() keys = %d, err = %v, want LONG, AFTER and NEW kept", len(values), err)
	}
}

func TestWriteValueWritesThroughASymlinkedSecretFile(t *testing.T) {
	target := writeSecretFile(t, "OTHER=1\n", 0o600)
	link := filepath.Join(t.TempDir(), "secrets.env")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := WriteValue(link, "GITHUB_TOKEN", "v"); err != nil {
		t.Fatalf("WriteValue() error = %v", err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("secret file link was replaced by a regular file")
	}
	values, err := ReadFile(target)
	if err != nil || values["GITHUB_TOKEN"] != "v" {
		t.Errorf("link target = %v, %v, want the token written through the link", values, err)
	}
}

func TestWriteValueDoesNotTouchAnExistingFileOthersCanRead(t *testing.T) {
	path := writeSecretFile(t, "OTHER=1\n", 0o644)

	err := WriteValue(path, "GITHUB_TOKEN", "v")

	assertErrorMentions(t, err, "0600")
	data, readErr := os.ReadFile(path)
	if readErr != nil || string(data) != "OTHER=1\n" {
		t.Errorf("file = %q, want it unchanged", data)
	}
}

func TestWriteValueCollapsesDuplicateLinesOfTheKeyIntoOne(t *testing.T) {
	path := writeSecretFile(t, "GITHUB_TOKEN=a\nOTHER=1\nGITHUB_TOKEN=b\n", 0o600)

	if err := WriteValue(path, "GITHUB_TOKEN", "new"); err != nil {
		t.Fatalf("WriteValue() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil || string(data) != "GITHUB_TOKEN=new\nOTHER=1\n" {
		t.Errorf("file = %q, want one GITHUB_TOKEN line", data)
	}
}

func TestVMEnvCollectsTheVarsOfWiredSecrets(t *testing.T) {
	plan := Plan{Wired: []Wire{
		{Name: "gitlab", Definition: Definition{Vars: map[string]string{"GITLAB_HOST": "gitlab.example.com"}}},
		{Name: "jira", Definition: Definition{Vars: map[string]string{"JIRA_SITE": "x.atlassian.net"}}},
	}}

	env, err := plan.VMEnv()

	want := map[string]string{"GITLAB_HOST": "gitlab.example.com", "JIRA_SITE": "x.atlassian.net"}
	if err != nil || !reflect.DeepEqual(env, want) {
		t.Errorf("VMEnv() = %v, %v, want %v", env, err, want)
	}
}

func TestVMEnvStopsWhenTwoSecretsGiveOneVarDifferentValues(t *testing.T) {
	plan := Plan{Wired: []Wire{
		{Name: "a", Definition: Definition{Vars: map[string]string{"HOST": "a.example.com"}}},
		{Name: "b", Definition: Definition{Vars: map[string]string{"HOST": "b.example.com"}}},
	}}

	_, err := plan.VMEnv()

	assertErrorMentions(t, err, "HOST")
}

// --- 作成時の宣言に残す形 ---

func TestTheRecordOfAWiredSecretHoldsEverythingButTheValue(t *testing.T) {
	plan := Plan{Wired: []Wire{{Name: "gitlab", Definition: Definition{
		Key: "GITLAB_TOKEN", Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Vars: map[string]string{"GITLAB_HOST": "gitlab.example.com"},
	}}}}

	got := plan.WiredSecrets()

	want := []WiredSecret{{Name: "gitlab", Key: "GITLAB_TOKEN", Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Vars: map[string]string{"GITLAB_HOST": "gitlab.example.com"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("WiredSecrets = %+v, want %+v", got, want)
	}
}

func TestSandboxSecretsCarryTheValueOfTheKey(t *testing.T) {
	plan := Plan{Wired: []Wire{{Name: "gitlab", Definition: Definition{Key: "GITLAB_TOKEN", Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN"}}}}

	got, err := plan.SandboxSecrets(Values{"GITLAB_TOKEN": "glpat_x"})

	want := []runtime.SandboxSecret{{Hosts: []string{"gitlab.example.com"}, Env: "GITLAB_TOKEN", Value: "glpat_x"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("SandboxSecrets = %+v, %v, want %+v", got, err, want)
	}
}

func TestSetupCommandNamesOnlyACommandThatWritesTheKeyOfTheDefinition(t *testing.T) {
	defs := map[string]Definition{
		GitHubName: {Service: "github", Key: "GITHUB_TOKEN", Hosts: []string{"api.github.com"}},
		"api":      {Key: "API_TOKEN", Hosts: []string{"api.example.com"}, Env: "API_TOKEN"},
		"shared-a": {Key: "A_TOKEN", Hosts: []string{"shared.example.com", "a.example.com"}, Env: "A_TOKEN"},
		"shared-b": {Key: "B_TOKEN", Hosts: []string{"shared.example.com"}, Env: "B_TOKEN"},
		"service":  {Service: "other", Key: "OTHER_TOKEN", Hosts: []string{"other.example.com"}},
	}
	for _, tt := range []struct {
		name, want string
		found      bool
	}{
		{name: GitHubName, want: "sbxr secret setup github", found: true},
		{name: "api", want: "sbxr secret setup custom --host api.example.com", found: true},
		// shared.example.com は key が 1 つに決まらないので、この定義だけが持つ host を案内する
		{name: "shared-a", want: "sbxr secret setup custom --host a.example.com", found: true},
		{name: "shared-b", found: false},
		{name: "service", found: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, found := SetupCommand(tt.name, defs)

			if got != tt.want || found != tt.found {
				t.Errorf("SetupCommand(%q) = %q, %v, want %q, %v", tt.name, got, found, tt.want, tt.found)
			}
		})
	}
}
