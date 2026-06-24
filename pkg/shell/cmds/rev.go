package cmds

import (
	"fmt"
	"io"
)

func CmdRev(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	lines, code := ReadInputLines(ctx, args, stdin, errW, "rev")
	if code != 0 {
		return code
	}
	for _, l := range lines {
		runes := []rune(l)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		fmt.Fprintln(w, string(runes))
	}
	return 0
}
