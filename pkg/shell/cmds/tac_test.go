package cmds_test

import (
	"strings"
	"testing"
)

func TestTac(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "seq 3 | tac")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || lines[0] != "3" || lines[2] != "1" {
		t.Errorf("tac: got %v", lines)
	}
}
