package cmds_test

import (
	"strings"
	"testing"
)

func TestNl(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo -e 'a\\nb\\nc' | nl")
	if !strings.Contains(out, "1") && !strings.Contains(out, "a") {
		t.Errorf("nl: got %q", out)
	}
}
