package cmds_test

import (
	"strings"
	"testing"
)

func TestSort(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "sort /docs/notes.txt")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != "apple" { t.Errorf("sort: first should be 'apple', got %q", lines[0]) }
}

func TestSortNumeric(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "sort -n /docs/numbers.txt")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != "1" { t.Errorf("sort -n: first should be '1', got %q", lines[0]) }
}

func TestSortWithFlags(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "sort -rn /docs/numbers.txt")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "30" {
		t.Errorf("sort -rn: first should be 30, got %q", lines[0])
	}
}
