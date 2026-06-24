package cmds_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/pkg/shell"
)

func TestEnv(t *testing.T) {
	sh := shell.NewShell(testFS())
	sh.SetEnv("FOO", "bar")
	var out bytes.Buffer
	sh.Exec("env", &out, &bytes.Buffer{}, nil)
	if !strings.Contains(out.String(), "FOO=bar") {
		t.Errorf("env: got %q", out.String())
	}
}

func TestPrintenv(t *testing.T) {
	sh := shell.NewShell(testFS())
	sh.SetEnv("MY_VAR", "hello")
	var out bytes.Buffer
	sh.Exec("printenv MY_VAR", &out, &bytes.Buffer{}, nil)
	if strings.TrimSpace(out.String()) != "hello" {
		t.Errorf("printenv: got %q", out.String())
	}
}
