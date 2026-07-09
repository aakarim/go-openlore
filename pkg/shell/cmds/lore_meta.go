package cmds

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/aakarim/go-openlore/pkg/okf"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// MetaExtender augments a `lore meta` record for one document. It receives the
// document's absolute display path (so it can resolve which docset owns it), its
// raw bytes, and its already-parsed frontmatter, and returns extra top-level
// fields to merge into the emitted JSON object. Returning nil adds nothing. The
// okf plugin registers one to annotate docs with OKF conformance where OKF
// applies, so read-side discovery agrees with write-side enforcement.
type MetaExtender func(absPath string, content []byte, frontmatter map[string]any) map[string]any

// metaExtenders is the registry of `lore meta` record extenders, applied in
// registration order.
var metaExtenders []MetaExtender

// RegisterMetaExtender adds an extender to `lore meta`. Called by the host when
// a plugin (e.g. okf) contributes one.
func RegisterMetaExtender(e MetaExtender) {
	metaExtenders = append(metaExtenders, e)
}

// cmdLoreMeta walks documents from the cwd (or an optional path argument) and
// emits each document's YAML frontmatter as one JSON object per line (NDJSON).
// It is a generic, read-side frontmatter reader: it emits any *.md that opens
// with a parseable frontmatter block and skips the rest — no `type`, no
// conformance, no OKF knowledge required. `path` is emitted relative to the
// walk root so it feeds straight back into cat/grep. Registered plugins may
// enrich each record via MetaExtenders.
func cmdLoreMeta(ctx CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	root := ctx.Cwd()
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(errW, "lore meta: unknown flag %q\n", a)
			return 1
		}
		root = ctx.Resolve(a)
	}
	root = vfs.CleanPath(root)

	type record struct {
		rel  string
		data map[string]any
	}
	var records []record

	err := vfs.WalkDir(ctx.FS(), root, func(p string, info *vfs.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil || info.Dir {
			return nil
		}
		if ok, _ := path.Match("*.md", info.FileName); !ok {
			return nil
		}
		content, err := ctx.FS().ReadFile(p)
		if err != nil {
			return nil
		}
		fm, _, hasFM, err := okf.ParseFrontmatter(content)
		if !hasFM || err != nil {
			return nil // no (or unparseable) frontmatter block: not a meta doc
		}

		data := make(map[string]any, len(fm)+1+len(metaExtenders))
		for k, v := range fm {
			data[k] = v
		}
		for _, ext := range metaExtenders {
			for k, v := range ext(p, content, fm) {
				data[k] = v
			}
		}
		data["path"] = relPath(root, p)
		records = append(records, record{rel: data["path"].(string), data: data})
		return nil
	})
	if err != nil {
		fmt.Fprintf(errW, "lore meta: %s\n", err)
		return 1
	}

	// Stable output regardless of the filesystem's walk order.
	sort.Slice(records, func(i, j int) bool { return records[i].rel < records[j].rel })
	enc := json.NewEncoder(w)
	for _, r := range records {
		if err := enc.Encode(r.data); err != nil {
			fmt.Fprintf(errW, "lore meta: %s\n", err)
			return 1
		}
	}
	return 0
}

// relPath returns p expressed relative to root (POSIX, no leading slash), so a
// `lore meta` path can be fed straight back into cat/grep from the cwd. p is
// assumed to be at or under root (it comes from a walk rooted there).
func relPath(root, p string) string {
	root = vfs.CleanPath(root)
	p = vfs.CleanPath(p)
	if root == "/" {
		return strings.TrimPrefix(p, "/")
	}
	if p == root {
		return path.Base(p)
	}
	return strings.TrimPrefix(p, root+"/")
}
