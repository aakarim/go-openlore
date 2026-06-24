package cmds

import (
	"fmt"
	"io"
)

func CmdCd(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	target := "/"
	if len(args) > 0 {
		target = args[0]
	}
	p := ctx.Resolve(target)
	f, err := ctx.FS().Stat(p)
	if err != nil {
		fmt.Fprintf(errW, "cd: %s: No such file or directory\n", target)
		return 1
	}
	if !f.Dir {
		fmt.Fprintf(errW, "cd: %s: Not a directory\n", target)
		return 1
	}
	ctx.SetCwd(p)
	return 0
}
