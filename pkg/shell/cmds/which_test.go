package cmds_test

import (
	"strings"
	"testing"
)

func TestWhich(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "which grep")
	if !strings.Contains(out, "built-in") {
		t.Errorf("which: got %q", out)
	}
}

func TestType(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "type grep")
	if !strings.Contains(out, "builtin") {
		t.Errorf("type: got %q", out)
	}
}
