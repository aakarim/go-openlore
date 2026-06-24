package cmds_test

import (
	"strings"
	"testing"
)

func TestTr(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo hello | tr 'a-z' 'A-Z'")
	if strings.TrimSpace(out) != "HELLO" {
		t.Errorf("tr: got %q", strings.TrimSpace(out))
	}
}
