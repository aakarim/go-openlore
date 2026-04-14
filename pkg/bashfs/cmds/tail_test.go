package cmds_test

import (
	"strings"
	"testing"
)

func TestTail(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "tail -n 3 /docs/numbers.txt")
	if !strings.Contains(out, "20") || !strings.Contains(out, "3") {
		t.Errorf("tail: got %q", out)
	}
}

func TestTailPipe(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "seq 10 | tail -n 3")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Should have the last 3 numbers from 1-10
	if len(lines) < 2 || !strings.Contains(out, "10") {
		t.Errorf("tail pipe: got %q", out)
	}
}
