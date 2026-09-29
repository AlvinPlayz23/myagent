package tools

import "testing"

func TestQuoteCmdEscapesControlCharacters(t *testing.T) {
	input := `x" & whoami | type %PATH% ^ <in >out (group)`
	want := `"x^" ^& whoami ^| type %%PATH%% ^^ ^<in ^>out ^(group^)"`
	if got := quoteCmd(input); got != want {
		t.Errorf("quoteCmd(%q) = %q, want %q", input, got, want)
	}
}
