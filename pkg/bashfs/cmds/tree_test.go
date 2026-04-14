package cmds_test

import (
	"strings"
	"testing"
)

func TestTree(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "tree /docs -L 1")
	if !strings.Contains(out, "readme.md") || !strings.Contains(out, "sub/") {
		t.Errorf("tree: got %q", out)
	}
}
