package cmds_test

import (
	"strings"
	"testing"
)

func TestCut(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "cut -d , -f 1 /docs/data.csv")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[1] != "alice" { t.Errorf("cut: got %q, want 'alice'", lines[1]) }
}

func TestCutMultipleFields(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "cut -d , -f 1,3 /docs/data.csv")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "name,city" {
		t.Errorf("cut -f 1,3: got %q", lines[0])
	}
}
