package cmds_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/pkg/bashfs"
)

func TestExport(t *testing.T) {
	sh := bashfs.NewShell(testFS())
	var out, errOut bytes.Buffer
	sh.Exec("export FOO=bar", &out, &errOut, nil)
	if sh.GetEnv("FOO") != "bar" { t.Errorf("export: got %q", sh.GetEnv("FOO")) }
}

func TestUnset(t *testing.T) {
	sh := bashfs.NewShell(testFS())
	sh.SetEnv("FOO", "bar")
	var out bytes.Buffer
	sh.Exec("unset FOO", &out, &bytes.Buffer{}, nil)
	if sh.GetEnv("FOO") != "" {
		t.Error("unset should remove variable")
	}
}

func TestSet(t *testing.T) {
	sh := bashfs.NewShell(testFS())
	sh.SetEnv("A", "1")
	var out bytes.Buffer
	sh.Exec("set", &out, &bytes.Buffer{}, nil)
	if !strings.Contains(out.String(), "A=") {
		t.Errorf("set: got %q", out.String())
	}
}
