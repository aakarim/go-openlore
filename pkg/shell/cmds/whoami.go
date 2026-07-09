package cmds

import (
	"fmt"
	"io"
)

func CmdWhoami(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	env := ctx.AllEnv()
	identity := env["OPENLORE_IDENTITY"]

	if identity == "" {
		// No resolved identity: report the generic shell principal.
		fmt.Fprintln(w, "lore")
		return 0
	}

	fmt.Fprintln(w, identity)
	return 0
}

func CmdHostname(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	fmt.Fprintln(w, "lore-shell")
	return 0
}
