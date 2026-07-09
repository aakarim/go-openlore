package cmds

import (
	"fmt"
	"io"
	"strings"
)

// CmdLore is the `lore` introspection dispatcher. Bare `lore` prints usage and
// exits 0; an unknown subcommand errors to stderr and exits 1. Subcommands
// report per-session facts the agent needs to navigate its access.
func CmdLore(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	if len(args) == 0 {
		printLoreUsage(w)
		return 0
	}
	switch args[0] {
	case "docsets":
		return cmdLoreDocsets(ctx, args[1:], w, errW)
	default:
		fmt.Fprintf(errW, "lore: unknown command %q\n", args[0])
		printLoreUsage(errW)
		return 1
	}
}

func printLoreUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: lore <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  docsets   List the docsets you can access, their paths, and attributes")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run 'lore <command>' for a specific view.")
}

// cmdLoreDocsets prints an aligned, greppable table of the session's accessible
// docsets: name, direct access (r/rw), attribute tokens (home,publish),
// and display paths.
func cmdLoreDocsets(ctx CmdContext, args []string, w io.Writer, errW io.Writer) int {
	docsets := ctx.Docsets()

	rows := [][4]string{{"DOCSET", "GRANT", "ATTRIBUTES", "PATHS"}}
	for _, d := range docsets {
		grant := d.Grant
		if grant == "" {
			grant = "ro"
		}
		var attrs []string
		if d.Home {
			attrs = append(attrs, "home")
		}
		if d.Inbox {
			attrs = append(attrs, "inbox")
		}
		attrStr := "-"
		if len(attrs) > 0 {
			attrStr = strings.Join(attrs, ",")
		}
		rows = append(rows, [4]string{d.Name, grant, attrStr, strings.Join(d.Paths, ",")})
	}

	// Column widths from every cell except the last column (which is ragged).
	var w0, w1, w2 int
	for _, r := range rows {
		if len(r[0]) > w0 {
			w0 = len(r[0])
		}
		if len(r[1]) > w1 {
			w1 = len(r[1])
		}
		if len(r[2]) > w2 {
			w2 = len(r[2])
		}
	}
	for _, r := range rows {
		fmt.Fprintf(w, "%-*s  %-*s  %-*s  %s\n", w0, r[0], w1, r[1], w2, r[2], r[3])
	}
	return 0
}
