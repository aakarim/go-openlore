package cmds

import (
	"fmt"
	"io"
)

func CmdTac(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	lines, code := ReadInputLines(ctx, args, stdin, errW, "tac")
	if code != 0 {
		return code
	}
	for i := len(lines) - 1; i >= 0; i-- {
		fmt.Fprintln(w, lines[i])
	}
	return 0
}
