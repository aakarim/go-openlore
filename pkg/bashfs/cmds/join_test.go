package cmds_test

import (
	"strings"
	"testing"
)

func TestJoin(t *testing.T) {
	fs := testFS()
	fs.AddFile("/docs/j1.txt", "a 1\nb 2\nc 3\n")
	fs.AddFile("/docs/j2.txt", "a x\nb y\nd z\n")
	out, _, _ := execCmd(t, fs, "join /docs/j1.txt /docs/j2.txt")
	if !strings.Contains(out, "a") {
		t.Errorf("join: got %q", out)
	}
}
