package cmds_test

import (
	"strings"
	"testing"
)

func TestMd5sum(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "md5sum /docs/checksum.txt")
	parts := strings.Fields(out)
	if len(parts) < 2 || len(parts[0]) != 32 {
		t.Errorf("md5sum: got %q", out)
	}
}

func TestSha256sum(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "sha256sum /docs/checksum.txt")
	parts := strings.Fields(out)
	if len(parts) < 2 || len(parts[0]) != 64 {
		t.Errorf("sha256sum: got %q", out)
	}
}

func TestSha1sum(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "sha1sum /docs/checksum.txt")
	parts := strings.Fields(out)
	if len(parts) < 2 || len(parts[0]) != 40 {
		t.Errorf("sha1sum: got %q", out)
	}
}
