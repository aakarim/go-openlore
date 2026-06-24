package cmds

import (
	"fmt"
	"io"
)

func CmdHistory(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	fmt.Fprintln(w, "history: not available in this shell")
	return 0
}

func CmdAlias(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	fmt.Fprintln(errW, "alias: not supported in this shell")
	return 1
}

func CmdUnalias(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	fmt.Fprintln(errW, "unalias: not supported in this shell")
	return 1
}
