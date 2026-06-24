package cmds

import (
	"fmt"
	"io"
)

func CmdWhoami(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	env := ctx.AllEnv()
	identity := env["OPENLORE_IDENTITY"]
	lore := env["OPENLORE_LORE"]

	if identity == "" && lore == "" {
		fmt.Fprintln(w, "lore")
		return 0
	}

	if identity == "" {
		identity = "anonymous"
	}
	if lore == "" {
		lore = "default"
	}

	fmt.Fprintf(w, "%s (lore: %s)\n", identity, lore)
	return 0
}

func CmdHostname(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	fmt.Fprintln(w, "lore-shell")
	return 0
}
