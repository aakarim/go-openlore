package cmds_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/pkg/bashfs"
)

func TestRead(t *testing.T) {
	sh := bashfs.NewShell(testFS())
	var out, errOut bytes.Buffer
	stdin := strings.NewReader("hello world\n")
	sh.Exec("read VAR1 VAR2", &out, &errOut, stdin)
	if sh.GetEnv("VAR1") != "hello" { t.Errorf("read VAR1: got %q", sh.GetEnv("VAR1")) }
	if sh.GetEnv("VAR2") != "world" { t.Errorf("read VAR2: got %q", sh.GetEnv("VAR2")) }
}
