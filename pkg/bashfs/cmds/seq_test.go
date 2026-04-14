package cmds_test

import "testing"

func TestSeq(t *testing.T) {
	assertOutput(t, testFS(), "seq 5", "1\n2\n3\n4\n5")
}

func TestSeqWithIncrement(t *testing.T) {
	assertOutput(t, testFS(), "seq 1 2 7", "1\n3\n5\n7")
}

func TestSeqSep(t *testing.T) {
	assertOutput(t, testFS(), "seq -s , 3", "1,2,3")
}
