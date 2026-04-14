package cmds

import (
	"fmt"
	"io"
	"strings"
)

func CmdTee(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	if stdin == nil {
		return 0
	}
	// In read-only filesystem, just pass through stdin to stdout
	// File arguments are ignored with a warning
	for _, a := range args {
		if a != "-a" && !strings.HasPrefix(a, "-") {
			fmt.Fprintf(errW, "tee: %s: read-only filesystem, file output ignored\n", a)
		}
	}
	data, _ := io.ReadAll(stdin)
	w.Write(data)
	return 0
}
