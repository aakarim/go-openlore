package cmds_test

import (
	"strings"
	"testing"
)

func TestDate(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "date '+%Y'")
	if len(strings.TrimSpace(out)) != 4 {
		t.Errorf("date: got %q", out)
	}
}

func TestDateUTC(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "date -u '+%Z'")
	if strings.TrimSpace(out) != "UTC" {
		t.Errorf("date -u: got %q", strings.TrimSpace(out))
	}
}
