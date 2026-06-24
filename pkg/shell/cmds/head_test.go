package cmds_test

import (
	"strings"
	"testing"
)

func TestHead(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "head -n 2 /docs/readme.md")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("head -n 2: got %d lines, want 2", len(lines))
	}
}
