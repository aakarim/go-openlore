package cmds_test

import "testing"

func TestWhoami(t *testing.T)   { assertOutput(t, testFS(), "whoami", "lore") }
func TestHostname(t *testing.T) { assertOutput(t, testFS(), "hostname", "lore-shell") }
