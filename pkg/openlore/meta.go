package openlore

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/aakarim/go-openlore/pkg/okf"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// This file holds the business logic behind `lore meta` — walking a document
// tree, extracting each document's frontmatter, and letting plugins enrich the
// result. It lives in the openlore package (not pkg/shell/cmds) because it is
// domain logic, not shell plumbing; the `cmds` package only owns the generic
// `lore` dispatcher. The thin command adapter (cmdLoreMeta) registers itself
// into that dispatcher via cmds.RegisterLoreSub at init.

// MetaExtender augments a scanned document's fields. It receives the document's
// absolute display path (so it can resolve which docset owns it), its raw bytes,
// and its already-parsed frontmatter, and returns extra fields to merge into the
// record. Returning nil adds nothing. The okf plugin registers one to annotate
// documents with OKF conformance where OKF applies, so read-side discovery
// agrees with write-side enforcement.
type MetaExtender func(absPath string, content []byte, frontmatter map[string]any) map[string]any

// metaExtenders is the process-wide set of extenders applied by `lore meta`,
// populated from plugins at registration (see registerPlugin).
var metaExtenders []MetaExtender

// registerMetaExtender adds an extender applied by the `lore meta` command.
func registerMetaExtender(e MetaExtender) { metaExtenders = append(metaExtenders, e) }

// MetaRecord is one scanned document: its path relative to the scan root and the
// merged field map (frontmatter plus any extender-contributed fields). Path is
// kept separate so callers decide how to surface it.
type MetaRecord struct {
	Path   string
	Fields map[string]any
}

// ScanMeta walks *.md documents under root in fsys and returns one MetaRecord
// per document that opens with a parseable YAML frontmatter block, sorted by
// path. It is a generic reader, not a validator: documents without (or with an
// unparseable) frontmatter block are skipped, and the full frontmatter map is
// passed through. Each record's Fields are the frontmatter merged with the
// output of every extender (applied in order). Paths are relative to root so
// they feed straight back into cat/grep from that directory.
//
// It is exported so downstream tooling can reuse the same frontmatter-indexing
// logic that backs `lore meta` (as pkg/okf backs write-side validation).
func ScanMeta(fsys vfs.FileSystem, root string, extenders ...MetaExtender) ([]MetaRecord, error) {
	root = vfs.CleanPath(root)
	var records []MetaRecord
	err := vfs.WalkDir(fsys, root, func(p string, info *vfs.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil || info.Dir {
			return nil
		}
		if ok, _ := path.Match("*.md", info.FileName); !ok {
			return nil
		}
		content, err := fsys.ReadFile(p)
		if err != nil {
			return nil
		}
		fm, _, hasFM, err := okf.ParseFrontmatter(content)
		if !hasFM || err != nil {
			return nil // no (or unparseable) frontmatter block: not a meta doc
		}
		fields := make(map[string]any, len(fm))
		for k, v := range fm {
			fields[k] = v
		}
		for _, ext := range extenders {
			for k, v := range ext(p, content, fm) {
				fields[k] = v
			}
		}
		records = append(records, MetaRecord{Path: relPath(root, p), Fields: fields})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	return records, nil
}

// cmdLoreMeta is the thin `lore meta` adapter: it parses the optional path
// argument, calls ScanMeta over the session filesystem (so read-scoping is
// inherited), and emits each record as one JSON object per line (NDJSON) with
// `path` merged in. All the real work is in ScanMeta.
func cmdLoreMeta(ctx cmds.CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
	root := ctx.Cwd()
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			fmt.Fprintf(errW, "lore meta: unknown flag %q\n", a)
			return 1
		}
		root = ctx.Resolve(a)
	}

	records, err := ScanMeta(ctx.FS(), root, metaExtenders...)
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

func init() {
	// `lore meta` lives here (openlore) rather than in cmds because it carries
	// domain logic; it plugs into the generic `lore` dispatcher owned by cmds.
	cmds.RegisterLoreSub(cmds.LoreSub{
		Name:    "meta",
		Summary: "Emit each document's frontmatter as JSON (NDJSON), cwd-scoped",
		Run:     cmdLoreMeta,
	})
}
