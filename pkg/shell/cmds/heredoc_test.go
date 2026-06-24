package cmds_test

import (
	"strings"
	"testing"
)

// Heredoc piped into cat should reach the next stage of the pipeline.
// This is the workflow used by `cat <<EOF | kb publish ...`.
func TestHeredocPipeIntoCat(t *testing.T) {
	out, _, code := execCmd(t, testFS(), "cat <<EOF\nhello\nworld\nEOF")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if out != "hello\nworld\n" {
		t.Errorf("got %q", out)
	}
}

func TestHeredocPipelineToWc(t *testing.T) {
	out, _, code := execCmd(t, testFS(), "cat <<EOF | wc -l\na\nb\nc\nEOF")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if strings.TrimSpace(out) != "3" {
		t.Errorf("expected 3, got %q", strings.TrimSpace(out))
	}
}

func TestHeredocQuotedDelimiterNoExpansion(t *testing.T) {
	out, _, code := execCmd(t, testFS(), "cat <<'EOF'\n$NOT_EXPANDED\nEOF")
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if out != "$NOT_EXPANDED\n" {
		t.Errorf("got %q", out)
	}
}

// Without heredoc support, the agent's transcript line
// `cat <<EOF | kb publish ...` would have parsed each body line as a
// separate command (`---: command not found`, `name:: command not found`).
// Verify that no such "command not found" messages appear.
func TestHeredocSuppressesPhantomCommands(t *testing.T) {
	_, errOut, _ := execCmd(t, testFS(),
		"cat <<EOF\n---\nname: foo\n---\n\nbody text\nEOF")
	if strings.Contains(errOut, "command not found") {
		t.Errorf("body lines leaked as commands; stderr:\n%s", errOut)
	}
}

// `kb publish --help 2>&1 | head` should not spawn a stray `1: command not
// found` from the `&1` being split off.
func TestStderrMergeNoPhantomCommand(t *testing.T) {
	// `unknown-cmd` doesn't exist; we just want to verify the 2>&1 didn't
	// produce a `1: command not found` line of its own.
	_, errOut, _ := execCmd(t, testFS(), "unknown-cmd 2>&1 | head -1")
	if strings.Contains(errOut, "1: command not found") {
		t.Errorf("stray '1: command not found' from 2>&1; stderr:\n%s", errOut)
	}
}

// With 2>&1, stderr from the left-hand command should be piped into the
// right-hand command via stdout.
func TestStderrMergePipesIntoNext(t *testing.T) {
	out, _, _ := execCmd(t, testFS(), "unknown-cmd 2>&1 | wc -l")
	// "command not found" + "Type 'help' ..." = 2 lines, both go into the pipe.
	if strings.TrimSpace(out) != "2" {
		t.Errorf("expected 2 lines piped, got %q", strings.TrimSpace(out))
	}
}
