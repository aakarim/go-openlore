package cmds_test

import (
	"strings"
	"testing"
)

func TestAwkFieldSep(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "awk -F , '{print $1}' /docs/data.csv")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != "name" {
		t.Errorf("awk -F,: got %q", lines[0])
	}
}

func TestAwkPipe(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "cat /docs/numbers.txt | awk '{sum += $1} END{print sum}'")
	if strings.TrimSpace(out) != "66" {
		t.Errorf("awk sum: got %q, want 66", strings.TrimSpace(out))
	}
}

func TestAwkRegexPattern(t *testing.T) {
	fs := testFS()
	out, _, code := execCmd(t, fs, "cat /docs/readme.md | awk '/^#/{print}'")
	if code != 0 {
		t.Fatalf("awk regex pattern failed: code=%d", code)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Errorf("awk /^#/: expected 1 header line, got %d lines:\n%s", len(lines), out)
	}
	if lines[0] != "# Hello World" {
		t.Errorf("awk /^#/: expected '# Hello World', got %q", lines[0])
	}
}

func TestAwkRegexDoesNotMatchAll(t *testing.T) {
	fs := testFS()
	// Pattern /^Line/ should only match lines starting with "Line"
	out, _, code := execCmd(t, fs, "cat /docs/readme.md | awk '/^Line/{print}'")
	if code != 0 {
		t.Fatalf("awk regex pattern failed: code=%d", code)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "Line") {
			t.Errorf("awk /^Line/: unexpected line %q", line)
		}
	}
	if len(lines) != 3 {
		t.Errorf("awk /^Line/: expected 3 lines, got %d", len(lines))
	}
}
