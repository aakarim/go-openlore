package cmds_test

import (
	"strings"
	"testing"
)

func TestHistory(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "history")
	if !strings.Contains(out, "not available") {
		t.Errorf("history: got %q", out)
	}
}

func TestAlias(t *testing.T) {
	assertExitCode(t, testFS(), "alias", 1)
}

func TestUnalias(t *testing.T) {
	assertExitCode(t, testFS(), "unalias foo", 1)
}
