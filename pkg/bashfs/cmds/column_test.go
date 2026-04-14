package cmds_test

import (
	"strings"
	"testing"
)

func TestColumn(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "column -t /docs/tabs.txt")
	if !strings.Contains(out, "col1") {
		t.Errorf("column: got %q", out)
	}
}
