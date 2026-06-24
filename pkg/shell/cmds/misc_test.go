package cmds_test

import (
	"strings"
	"testing"
)

func TestTrue(t *testing.T)  { assertExitCode(t, testFS(), "true", 0) }
func TestFalse(t *testing.T) { assertExitCode(t, testFS(), "false", 1) }

func TestClear(t *testing.T) {
	out, _, code := execCmd(t, testFS(), "clear")
	if code != 0 || !strings.Contains(out, "\033[") {
		t.Errorf("clear: code=%d out=%q", code, out)
	}
}

func TestSleep(t *testing.T) {
	_, errOut, code := execCmd(t, testFS(), "sleep 1")
	if code != 0 {
		t.Errorf("sleep: code=%d", code)
	}
	if !strings.Contains(errOut, "not supported") {
		t.Errorf("sleep: stderr=%q", errOut)
	}
}

func TestTimeout(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "timeout 5 echo hello")
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("timeout: got %q", out)
	}
}

func TestTime(t *testing.T) {
	_, errOut, _ := execCmd(t, testFS(), "time echo hello")
	if !strings.Contains(errOut, "real") {
		t.Errorf("time: stderr=%q", errOut)
	}
}
