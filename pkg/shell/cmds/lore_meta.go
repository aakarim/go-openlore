package cmds

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aakarim/go-openlore/pkg/meta"
)

// cmdLoreMeta is the `lore meta` command adapter. It is shell plumbing only: it
// parses the optional path argument, calls meta.Scan over the session
// filesystem (so read-scoping is inherited) with the session's plugin-provided
// extenders, and emits each record as one JSON object per line (NDJSON) with
// `path` merged in. All the real work lives in pkg/meta.
func cmdLoreMeta(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	root := ctx.Cwd()
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(errW, "lore meta: unknown flag %q\n", a)
			return 1
		}
		root = ctx.Resolve(a)
	}

	records, err := meta.Scan(ctx.FS(), root, ctx.MetaExtenders()...)
	if err != nil {
		fmt.Fprintf(errW, "lore meta: %s\n", err)
		return 1
	}

	enc := json.NewEncoder(w)
	for _, r := range records {
		obj := make(map[string]any, len(r.Fields)+1)
		for k, v := range r.Fields {
			obj[k] = v
		}
		obj["path"] = r.Path
		if err := enc.Encode(obj); err != nil {
			fmt.Fprintf(errW, "lore meta: %s\n", err)
			return 1
		}
	}
	return 0
}

func init() {
	RegisterLoreSub(LoreSub{
		Name:    "meta",
		Summary: "Emit each document's frontmatter as JSON (NDJSON), cwd-scoped",
		Run:     cmdLoreMeta,
	})
}
