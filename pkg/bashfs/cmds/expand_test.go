package cmds_test

import (
	"strings"
	"testing"
)

func TestExpand(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "expand /docs/tabs.txt")
	if strings.Contains(out, "\t") {
		t.Error("expand should replace tabs with spaces")
	}
}

func TestUnexpand(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo '        hello' | unexpand")
	if !strings.Contains(out, "\t") {
		t.Errorf("unexpand: got %q", out)
	}
}
