package cmds_test

import (
	"strings"
	"testing"
)

func TestTee(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo hello | tee")
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("tee: got %q", out)
	}
}
