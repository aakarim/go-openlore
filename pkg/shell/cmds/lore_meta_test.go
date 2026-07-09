package cmds_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
)

// metaFS builds a small doc tree with mixed frontmatter shapes.
func metaFS() *mapFS {
	fs := newMapFS()
	fs.AddDir("/")
	fs.AddDir("/docs")
	fs.AddFile("/docs/orders.md", "---\ntype: Table\ntitle: Orders\ntags:\n  - sales\n  - orders\n---\n# Orders\nbody\n")
	fs.AddFile("/docs/metric.md", "---\ntype: Metric\ntitle: Revenue\n---\nbody\n")
	fs.AddFile("/docs/plain.md", "# No frontmatter here\njust text\n")
	fs.AddFile("/docs/notes.txt", "---\ntype: NotMarkdown\n---\n")
	fs.AddDir("/docs/sub")
	fs.AddFile("/docs/sub/nested.md", "---\ntype: Note\n---\nnested body\n")
	return fs
}

// runMeta executes a command line in a shell rooted at cwd over metaFS.
func runMeta(t *testing.T, cwd, cmd string) (string, string, int) {
	t.Helper()
	sh := shell.NewShell(metaFS())
	if cwd != "" {
		sh.SetCwd(cwd)
	}
	var out, errOut bytes.Buffer
	code := sh.ExecPipeline(cmd, &out, &errOut, nil)
	return out.String(), errOut.String(), code
}

// parseNDJSON parses each non-empty line of NDJSON output into a map.
func parseNDJSON(t *testing.T, s string) []map[string]any {
	t.Helper()
	var recs []map[string]any
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("invalid NDJSON line %q: %v", line, err)
		}
		recs = append(recs, m)
	}
	return recs
}

func TestLoreMeta_EmitsFrontmatterNDJSON(t *testing.T) {
	out, errOut, code := runMeta(t, "/docs", "lore meta")
	if code != 0 {
		t.Fatalf("lore meta exit=%d stderr=%s", code, errOut)
	}
	recs := parseNDJSON(t, out)
	// orders.md, metric.md, sub/nested.md have frontmatter; plain.md and the
	// .txt do not qualify.
	if len(recs) != 3 {
		t.Fatalf("want 3 records, got %d:\n%s", len(recs), out)
	}
	// Deterministic sort by path.
	if recs[0]["path"] != "metric.md" || recs[1]["path"] != "orders.md" || recs[2]["path"] != "sub/nested.md" {
		t.Fatalf("paths not sorted/relative as expected: %v %v %v", recs[0]["path"], recs[1]["path"], recs[2]["path"])
	}
	// Full frontmatter passes through.
	if recs[1]["type"] != "Table" || recs[1]["title"] != "Orders" {
		t.Fatalf("orders.md frontmatter not emitted: %v", recs[1])
	}
	tags, ok := recs[1]["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "sales" {
		t.Fatalf("orders.md tags not emitted: %v", recs[1]["tags"])
	}
}

func TestLoreMeta_SkipsDocsWithoutFrontmatter(t *testing.T) {
	out, _, _ := runMeta(t, "/docs", "lore meta")
	if strings.Contains(out, "plain.md") {
		t.Fatalf("doc without frontmatter should be skipped:\n%s", out)
	}
	if strings.Contains(out, "notes.txt") {
		t.Fatalf("non-.md file should be skipped:\n%s", out)
	}
}

func TestLoreMeta_CwdScoped(t *testing.T) {
	// From /docs/sub, only nested.md is in scope; path is relative to cwd.
	out, _, code := runMeta(t, "/docs/sub", "lore meta")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	recs := parseNDJSON(t, out)
	if len(recs) != 1 || recs[0]["path"] != "nested.md" {
		t.Fatalf("cwd-scoped walk wrong: %v", recs)
	}
}

func TestLoreMeta_PathArgument(t *testing.T) {
	// cwd is /, but the path arg narrows to /docs/sub.
	out, _, code := runMeta(t, "/", "lore meta docs/sub")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	recs := parseNDJSON(t, out)
	if len(recs) != 1 || recs[0]["path"] != "nested.md" {
		t.Fatalf("path arg scoping wrong: %v", recs)
	}
}

func TestLoreMeta_UnknownFlagErrors(t *testing.T) {
	_, errOut, code := runMeta(t, "/docs", "lore meta --json")
	if code != 1 {
		t.Fatalf("unknown flag exit=%d, want 1", code)
	}
	if !strings.Contains(errOut, "unknown flag") {
		t.Fatalf("stderr missing flag error: %s", errOut)
	}
}

func TestLoreMeta_ExtenderMergesFields(t *testing.T) {
	// Register an extender that tags every metric doc; clean up after.
	cmds.RegisterMetaExtender(func(absPath string, content []byte, fm map[string]any) map[string]any {
		if fm["type"] == "Metric" {
			return map[string]any{"annotated": true}
		}
		return nil
	})
	t.Cleanup(cmds.ResetMetaExtendersForTest)

	out, _, code := runMeta(t, "/docs", "lore meta")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	recs := parseNDJSON(t, out)
	for _, r := range recs {
		if r["path"] == "metric.md" {
			if r["annotated"] != true {
				t.Fatalf("extender field not merged into metric.md: %v", r)
			}
		} else if _, ok := r["annotated"]; ok {
			t.Fatalf("extender leaked onto %v", r["path"])
		}
	}
}

func TestLore_UsageListsMeta(t *testing.T) {
	sh := shell.NewShell(metaFS())
	var out bytes.Buffer
	sh.ExecPipeline("lore", &out, &out, nil)
	if !strings.Contains(out.String(), "meta") || !strings.Contains(out.String(), "docsets") {
		t.Fatalf("lore usage should list core subcommands:\n%s", out.String())
	}
}

func TestRegisterLoreSub_PluginCanAddSubcommand(t *testing.T) {
	cmds.RegisterLoreSub(cmds.LoreSub{
		Name:    "ping",
		Summary: "test subcommand",
		Run: func(ctx cmds.CmdContext, args []string, w io.Writer, errW io.Writer, stdin io.Reader) int {
			io.WriteString(w, "pong\n")
			return 0
		},
	})
	t.Cleanup(func() { cmds.DeleteLoreSubForTest("ping") })

	sh := shell.NewShell(metaFS())
	var out, errOut bytes.Buffer
	code := sh.ExecPipeline("lore ping", &out, &errOut, nil)
	if code != 0 {
		t.Fatalf("registered subcommand exit=%d stderr=%s", code, errOut.String())
	}
}
