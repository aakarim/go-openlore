package cmds_test

import (
	"strings"
	"testing"
)

func TestUniq(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "sort /docs/notes.txt | uniq")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 { t.Errorf("uniq: got %d lines, want 4", len(lines)) }
}

func TestUniqCount(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "sort /docs/notes.txt | uniq -c")
	if !strings.Contains(out, "2") {
		t.Errorf("uniq -c: got %q", out)
	}
}
