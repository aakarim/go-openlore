package cmds_test

import (
	"strings"
	"testing"
)

func TestFold(t *testing.T) {
	fs := testFS()
	fs.AddFile("/docs/long.txt", "This is a very long line that should be wrapped at a certain width for display\n")
	out, _, _ := execCmd(t, fs, "fold -w 20 /docs/long.txt")
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if len(l) > 20 {
			t.Errorf("fold: line too long: %q", l)
		}
	}
}
