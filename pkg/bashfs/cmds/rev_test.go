package cmds_test

import (
	"strings"
	"testing"
)

func TestRev(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo hello | rev")
	if strings.TrimSpace(out) != "olleh" { t.Errorf("rev: got %q", strings.TrimSpace(out)) }
}
