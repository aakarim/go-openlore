package cmds_test

import (
	"strings"
	"testing"
)

func TestWc(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "wc /docs/numbers.txt")
	if !strings.Contains(out, "6") {
		t.Errorf("wc: got %q", out)
	}
}

func TestWcPipe(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "echo 'a b c' | wc")
	if !strings.Contains(out, "3") {
		t.Errorf("wc pipe: got %q", out)
	}
}

func TestWcFlags(t *testing.T) {
	fs := testFS()

	t.Run("wc -l", func(t *testing.T) {
		out, errOut, code := execCmd(t, fs, "wc -l /docs/readme.md")
		if code != 0 {
			t.Fatalf("wc -l failed: code=%d stderr=%s", code, errOut)
		}
		out = strings.TrimSpace(out)
		if !strings.HasPrefix(out, "5") {
			t.Errorf("wc -l: got %q, want line count starting with 5", out)
		}
		if !strings.HasSuffix(out, "/docs/readme.md") {
			t.Errorf("wc -l: got %q, want suffix /docs/readme.md", out)
		}
	})

	t.Run("wc -w", func(t *testing.T) {
		out, _, code := execCmd(t, fs, "wc -w /docs/readme.md")
		if code != 0 {
			t.Fatalf("wc -w failed: code=%d", code)
		}
		out = strings.TrimSpace(out)
		if !strings.HasSuffix(out, "/docs/readme.md") {
			t.Errorf("wc -w: got %q, want suffix /docs/readme.md", out)
		}
		// Should only show word count, not lines or bytes
		parts := strings.Fields(out)
		if len(parts) != 2 {
			t.Errorf("wc -w: expected 2 fields (count + filename), got %d: %q", len(parts), out)
		}
	})

	t.Run("wc -c", func(t *testing.T) {
		out, _, code := execCmd(t, fs, "wc -c /docs/readme.md")
		if code != 0 {
			t.Fatalf("wc -c failed: code=%d", code)
		}
		parts := strings.Fields(strings.TrimSpace(out))
		if len(parts) != 2 {
			t.Errorf("wc -c: expected 2 fields, got %d: %q", len(parts), out)
		}
	})

	t.Run("wc no flags", func(t *testing.T) {
		out, _, code := execCmd(t, fs, "wc /docs/readme.md")
		if code != 0 {
			t.Fatalf("wc failed: code=%d", code)
		}
		parts := strings.Fields(strings.TrimSpace(out))
		// Should have lines, words, bytes, filename = 4 fields
		if len(parts) != 4 {
			t.Errorf("wc (no flags): expected 4 fields (lines words bytes file), got %d: %q", len(parts), out)
		}
	})

	t.Run("wc -l from stdin", func(t *testing.T) {
		out, _, _ := execCmd(t, fs, "cat /docs/readme.md | wc -l")
		parts := strings.Fields(strings.TrimSpace(out))
		if len(parts) != 1 {
			t.Errorf("wc -l stdin: expected 1 field, got %d: %q", len(parts), out)
		}
	})
}

func TestWcTotal(t *testing.T) {
	fs := testFS()
	out, _, code := execCmd(t, fs, "wc -l /docs/readme.md /docs/notes.txt")
	if code != 0 {
		t.Fatalf("wc -l multiple files failed: code=%d", code)
	}
	if !strings.Contains(out, "total") {
		t.Errorf("wc with multiple files should print a total line, got:\n%s", out)
	}
}
