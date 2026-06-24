package cmds

import (
	"fmt"
	"io"
)

// VersionString is set by the server at init time.
var VersionString = "unknown"

// CmdVersion prints the OpenLore version.
func CmdVersion(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	fmt.Fprintf(w, "OpenLore %s\n", VersionString)
	return 0
}
