package cmds_test

import "testing"

func TestBasename(t *testing.T) { assertOutput(t, testFS(), "basename /docs/readme.md", "readme.md") }
func TestDirname(t *testing.T)  { assertOutput(t, testFS(), "dirname /docs/readme.md", "/docs") }
