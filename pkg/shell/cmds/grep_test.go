package cmds_test

import (
	"strings"
	"testing"
)

func TestPipe(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "cat /docs/notes.txt | grep apple")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("pipe: got %d lines, want 2", len(lines))
	}
}

func TestGrepLineNumbers(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "grep -n apple /docs/notes.txt")
	if !strings.Contains(out, "2:") {
		t.Errorf("grep -n: got %q", out)
	}
}

func TestGrepOnlyMatching(t *testing.T) {
	fs := testFS()

	t.Run("grep -o", func(t *testing.T) {
		out, _, code := execCmd(t, fs, "grep -o 'apple' /docs/notes.txt")
		if code != 0 {
			t.Fatalf("grep -o failed: code=%d", code)
		}
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		for _, line := range lines {
			if line != "apple" {
				t.Errorf("grep -o: expected each line to be 'apple', got %q", line)
			}
		}
		if len(lines) != 2 {
			t.Errorf("grep -o: expected 2 matches, got %d", len(lines))
		}
	})

	t.Run("grep -oh recursive", func(t *testing.T) {
		out, _, code := execCmd(t, fs, "grep -roh 'apple' /docs")
		if code != 0 {
			t.Fatalf("grep -roh failed: code=%d", code)
		}
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		for _, line := range lines {
			if line != "apple" {
				t.Errorf("grep -roh: expected 'apple', got %q", line)
			}
		}
	})

	t.Run("grep -c count", func(t *testing.T) {
		out, _, code := execCmd(t, fs, "grep -c apple /docs/notes.txt")
		if code != 0 {
			t.Fatalf("grep -c failed: code=%d", code)
		}
		if strings.TrimSpace(out) != "2" {
			t.Errorf("grep -c: expected '2', got %q", strings.TrimSpace(out))
		}
	})

	t.Run("grep -v invert", func(t *testing.T) {
		out, _, code := execCmd(t, fs, "grep -v apple /docs/notes.txt")
		if code != 0 {
			t.Fatalf("grep -v failed: code=%d", code)
		}
		if strings.Contains(out, "apple") {
			t.Errorf("grep -v: should not contain 'apple', got:\n%s", out)
		}
	})

	t.Run("grep -l files with matches", func(t *testing.T) {
		out, _, code := execCmd(t, fs, "grep -rl apple /docs")
		if code != 0 {
			t.Fatalf("grep -rl failed: code=%d", code)
		}
		if !strings.Contains(out, "notes.txt") {
			t.Errorf("grep -rl: should list notes.txt, got:\n%s", out)
		}
	})
}

func TestGrepRegex(t *testing.T) {
	fs := testFS()
	out, _, code := execCmd(t, fs, "grep '^#' /docs/readme.md")
	if code != 0 {
		t.Fatalf("grep regex failed: code=%d", code)
	}
	if !strings.Contains(out, "# Hello World") {
		t.Errorf("grep '^#': should match header, got:\n%s", out)
	}
}

func TestGrepOnlyMatchingPipe(t *testing.T) {
	fs := testFS()
	out, _, _ := execCmd(t, fs, "grep -roh 'apple' /docs | sort | uniq -c | sort -rn")
	out = strings.TrimSpace(out)
	if out == "" {
		t.Error("grep -roh pipeline returned empty output")
	}
	if !strings.Contains(out, "apple") {
		t.Errorf("grep -roh pipeline should contain 'apple', got: %q", out)
	}
}
