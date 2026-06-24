package cmds_test

import "testing"

func TestDiff(t *testing.T) {
	_, _, code := execCmd(t, testFS(), "diff /docs/sorted1.txt /docs/sorted2.txt")
	if code == 0 {
		t.Error("diff: different files should return 1")
	}
}

func TestDiffSame(t *testing.T) {
	_, _, code := execCmd(t, testFS(), "diff /docs/sorted1.txt /docs/sorted1.txt")
	if code != 0 {
		t.Error("diff: same file should return 0")
	}
}
