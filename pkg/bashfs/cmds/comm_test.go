package cmds_test

import (
	"strings"
	"testing"
)

func TestComm(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "comm /docs/sorted1.txt /docs/sorted2.txt")
	if !strings.Contains(out, "apple") {
		t.Errorf("comm: got %q", out)
	}
}
