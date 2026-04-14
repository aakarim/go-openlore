package cmds_test

import "testing"

func TestTestBuiltin(t *testing.T) {
	assertExitCode(t, testFS(), "test -f /docs/readme.md", 0)
	assertExitCode(t, testFS(), "test -d /docs", 0)
	assertExitCode(t, testFS(), "test -f /nonexistent", 1)
}

func TestBracket(t *testing.T) {
	assertExitCode(t, testFS(), "[ -f /docs/readme.md ]", 0)
}
