package cmds_test

import (
	"strings"
	"testing"
)

func TestEchoN(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo -n hello")
	if out != "hello" {
		t.Errorf("echo -n: got %q", out)
	}
}

func TestEchoEscapes(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo -e 'a\\tb'")
	if !strings.Contains(out, "\t") {
		t.Errorf("echo -e tab: got %q", out)
	}
}
