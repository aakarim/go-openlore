package cmds

import (
	"fmt"
	"io"
	"path"
	"strings"
)

func CmdBasename(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	if len(args) == 0 {
		fmt.Fprintln(errW, "basename: missing operand")
		return 1
	}
	name := path.Base(args[0])
	if len(args) > 1 {
		suffix := args[1]
		name = strings.TrimSuffix(name, suffix)
	}
	fmt.Fprintln(w, name)
	return 0
}

func CmdDirname(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	if len(args) == 0 {
		fmt.Fprintln(errW, "dirname: missing operand")
		return 1
	}
	fmt.Fprintln(w, path.Dir(args[0]))
	return 0
}
