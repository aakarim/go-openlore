package cmds_test

import (
	"strings"
	"testing"
)

func TestXargs(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo readme.md | xargs echo found:")
	if !strings.Contains(out, "found:") || !strings.Contains(out, "readme.md") {
		t.Errorf("xargs: got %q", out)
	}
}

func TestXargsI(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo hello | xargs -I {} echo 'got: {}'")
	if !strings.Contains(out, "got: hello") {
		t.Errorf("xargs -I: got %q", out)
	}
}
