package herdr

import (
	"reflect"
	"testing"
)

func TestParseMachinesReadsTheListOutputOfHerdr(t *testing.T) {
	// herdr 0.9.1 の herdr machine list --json の出力の形
	out := []byte(`[{"id":"ab12","label":"app","target":"app.sbx","session":"default","enabled":false,"selected":false}]`)

	machines, err := ParseMachines(out)

	want := []Machine{{ID: "ab12", Target: "app.sbx", Enabled: false}}
	if err != nil || !reflect.DeepEqual(machines, want) {
		t.Errorf("ParseMachines = %v, %v, want %v", machines, err, want)
	}
}

func TestParseMachinesRejectsOutputThatIsNotAList(t *testing.T) {
	if _, err := ParseMachines([]byte("error: no server")); err == nil {
		t.Error("ParseMachines succeeded, want an error for unreadable output")
	}
}

func TestRecoveryCommandsAreTheHerdrCommandLines(t *testing.T) {
	for got, want := range map[string]string{
		AddCommand("app.sbx", "app"): "herdr machine add app.sbx --label app",
		EnableCommand("ab12"):        "herdr machine enable ab12",
		RemoveCommand("ab12"):        "herdr machine remove ab12",
	} {
		if got != want {
			t.Errorf("command = %q, want %q", got, want)
		}
	}
}
