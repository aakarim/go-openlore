package cmds_test

import (
	"strings"
	"testing"
)

func TestLs(t *testing.T) {
	fs := testFS()
	out, _, code := execCmd(t, fs, "ls /docs")
	if code != 0 { t.Fatalf("ls failed with code %d", code) }
	if !strings.Contains(out, "readme.md") { t.Error("ls /docs should list readme.md") }
}
