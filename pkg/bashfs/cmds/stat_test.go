package cmds_test

import (
	"strings"
	"testing"
)

func TestStat(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "stat /docs/readme.md")
	if !strings.Contains(out, "File:") || !strings.Contains(out, "Size:") {
		t.Errorf("stat: got %q", out)
	}
}

func TestStatDir(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "stat /docs")
	if !strings.Contains(out, "directory") {
		t.Errorf("stat dir: got %q", out)
	}
}

func TestStatShowsFullPath(t *testing.T) {
	fs := testFS()
	out, _, code := execCmd(t, fs, "stat /docs/readme.md")
	if code != 0 {
		t.Fatalf("stat failed: code=%d", code)
	}
	if !strings.Contains(out, "File: /docs/readme.md") {
		t.Errorf("stat should show full path /docs/readme.md, got:\n%s", out)
	}
}
