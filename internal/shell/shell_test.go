package shell

import "testing"

func TestQuoteKeepsASingleQuoteInsideOneArgument(t *testing.T) {
	if got := Quote("it's"); got != `'it'\''s'` {
		t.Errorf("Quote = %s, want the quote closed, escaped and reopened", got)
	}
}

func TestJoinQuotesOnlyTheArgumentsTheShellWouldSplitOrExpand(t *testing.T) {
	got := Join([]string{"herdr", "workspace", "create", "--cwd", "/Users/u/My Projects/$app", "--focus", ""})

	if want := `herdr workspace create --cwd '/Users/u/My Projects/$app' --focus ''`; got != want {
		t.Errorf("Join = %s, want %s", got, want)
	}
}
