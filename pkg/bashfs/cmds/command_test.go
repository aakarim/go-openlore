package cmds_test

import (
	"strings"
	"testing"
)

func TestCommand(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "command -v grep")
	if strings.TrimSpace(out) != "grep" {
		t.Errorf("command -v: got %q", out)
	}
}

func TestCommandExec(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "command echo hi")
	if strings.TrimSpace(out) != "hi" {
		t.Errorf("command exec: got %q", out)
	}
}
