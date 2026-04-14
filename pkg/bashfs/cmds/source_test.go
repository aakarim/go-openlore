package cmds_test

import (
	"strings"
	"testing"
)

func TestSource(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "source /docs/script.sh")
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") { t.Errorf("source: got %q", out) }
}

func TestEval(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "eval echo hello world")
	if strings.TrimSpace(out) != "hello world" { t.Errorf("eval: got %q", out) }
}
