package cmds

import (
	"fmt"
	"io"
)

func CmdEnv(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	if ctx.AllEnv() == nil {
		return 0
	}
	for k, v := range ctx.AllEnv() {
		fmt.Fprintf(w, "%s=%s\n", k, v)
	}
	return 0
}

func CmdPrintenv(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	if len(args) == 0 {
		return CmdEnv(ctx, args, w, errW, stdin)
	}
	for _, name := range args {
		val := ctx.GetEnv(name)
		if val != "" {
			fmt.Fprintln(w, val)
		} else {
			return 1
		}
	}
	return 0
}
