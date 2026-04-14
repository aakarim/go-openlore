package cmds_test

import "testing"

func TestExpr(t *testing.T)         { assertOutput(t, testFS(), "expr 2 + 3", "5") }
func TestExprMultiply(t *testing.T) { assertOutput(t, testFS(), "expr 4 '*' 5", "20") }
