package cmds_test

import (
	"strings"
	"testing"
)

func TestBase64(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo -n hello | base64")
	if strings.TrimSpace(out) != "aGVsbG8=" { t.Errorf("base64: got %q", strings.TrimSpace(out)) }
}

func TestBase64Decode(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo -n aGVsbG8= | base64 -d")
	if out != "hello" {
		t.Errorf("base64 -d: got %q", out)
	}
}
