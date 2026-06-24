package cmds_test

import (
	"strings"
	"testing"
)

func TestPaste(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "paste /docs/sorted1.txt /docs/sorted2.txt")
	if !strings.Contains(out, "apple\tbanana") {
		t.Errorf("paste: got %q", out)
	}
}
