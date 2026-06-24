package cmds_test

import "testing"

func TestCat(t *testing.T) {
	assertOutput(t, testFS(), "cat /docs/readme.md", "# Hello World\nThis is a test file.\nLine 3\nLine 4\nLine 5\n")
}
