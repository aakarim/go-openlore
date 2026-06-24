package cmds_test

import "testing"

func TestPrintf(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "printf '%s is %d years old\\n' alice 30")
	if out != "alice is 30 years old\n" {
		t.Errorf("printf: got %q", out)
	}
}
