package cmds_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/pkg/bashfs"
)

func TestCd(t *testing.T) {
	sh := bashfs.NewShell(testFS())
	var out, errOut bytes.Buffer
	sh.Exec("cd /docs", &out, &errOut, nil)
	out.Reset()
	sh.Exec("pwd", &out, &errOut, nil)
	if strings.TrimSpace(out.String()) != "/docs" {
		t.Errorf("cd+pwd: got %q", out.String())
	}
}

func TestCdNotFound(t *testing.T) {
	assertExitCode(t, testFS(), "cd /nonexistent", 1)
}
