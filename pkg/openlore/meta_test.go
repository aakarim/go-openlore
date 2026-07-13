package openlore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// metaTreeFS writes a small doc tree to a temp dir and returns a DirFS over it.
func metaTreeFS(t *testing.T) vfs.FileSystem {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/orders.md", "---\ntype: Table\ntitle: Orders\ntags:\n  - sales\n  - orders\n---\n# Orders\nbody\n")
	write("docs/metric.md", "---\ntype: Metric\ntitle: Revenue\n---\nbody\n")
	write("docs/plain.md", "# No frontmatter here\njust text\n")
	write("docs/notes.txt", "---\ntype: NotMarkdown\n---\n")
	write("docs/sub/nested.md", "---\ntype: Note\n---\nnested body\n")
	return NewDirFS(dir, config.FilesConfig{})
}

func TestScanMeta_EmitsFrontmatterSkipsRest(t *testing.T) {
	recs, err := ScanMeta(metaTreeFS(t), "/docs")
	if err != nil {
		t.Fatal(err)
	}
	// orders.md, metric.md, sub/nested.md qualify; plain.md and .txt do not.
	if len(recs) != 3 {
		t.Fatalf("want 3 records, got %d: %+v", len(recs), recs)
	}
	// Sorted by path.
	if recs[0].Path != "metric.md" || recs[1].Path != "orders.md" || recs[2].Path != "sub/nested.md" {
		t.Fatalf("paths not sorted/relative: %q %q %q", recs[0].Path, recs[1].Path, recs[2].Path)
	}
	// Full frontmatter passes through.
	if recs[1].Fields["type"] != "Table" || recs[1].Fields["title"] != "Orders" {
		t.Fatalf("orders.md fields wrong: %v", recs[1].Fields)
	}
	tags, ok := recs[1].Fields["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "sales" {
		t.Fatalf("orders.md tags wrong: %v", recs[1].Fields["tags"])
	}
}

func TestScanMeta_Scoped(t *testing.T) {
	recs, err := ScanMeta(metaTreeFS(t), "/docs/sub")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Path != "nested.md" {
		t.Fatalf("scoped scan wrong: %+v", recs)
	}
}

func TestScanMeta_AppliesExtenders(t *testing.T) {
	ext := func(absPath string, content []byte, fm map[string]any) map[string]any {
		if fm["type"] == "Metric" {
			return map[string]any{"annotated": true}
		}
		return nil
	}
	recs, err := ScanMeta(metaTreeFS(t), "/docs", ext)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Path == "metric.md" {
			if r.Fields["annotated"] != true {
				t.Fatalf("extender not applied to metric.md: %v", r.Fields)
			}
		} else if _, ok := r.Fields["annotated"]; ok {
			t.Fatalf("extender leaked onto %s", r.Path)
		}
	}
}

// runMeta drives the `lore meta` command through a real shell over fsys.
func runMeta(t *testing.T, fsys vfs.FileSystem, cwd, cmd string) (string, string, int) {
	t.Helper()
	sh := shell.NewShell(fsys)
	if cwd != "" {
		sh.SetCwd(cwd)
	}
	var out, errOut bytes.Buffer
	code := sh.ExecPipeline(cmd, &out, &errOut, nil)
	return out.String(), errOut.String(), code
}

func parseNDJSON(t *testing.T, s string) []map[string]any {
	t.Helper()
	var recs []map[string]any
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("invalid NDJSON %q: %v", line, err)
		}
		recs = append(recs, m)
	}
	return recs
}

func TestLoreMetaCommand_NDJSONWithRelativePaths(t *testing.T) {
	metaExtenders = nil // no plugin extenders for this test
	out, errOut, code := runMeta(t, metaTreeFS(t), "/docs", "lore meta")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	recs := parseNDJSON(t, out)
	if len(recs) != 3 {
		t.Fatalf("want 3 records, got %d:\n%s", len(recs), out)
	}
	if recs[0]["path"] != "metric.md" || recs[2]["path"] != "sub/nested.md" {
		t.Fatalf("paths wrong: %v", recs)
	}
	if strings.Contains(out, "plain.md") || strings.Contains(out, "notes.txt") {
		t.Fatalf("non-qualifying docs leaked:\n%s", out)
	}
}

func TestLoreMetaCommand_PathArgument(t *testing.T) {
	metaExtenders = nil
	out, _, code := runMeta(t, metaTreeFS(t), "/", "lore meta docs/sub")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	recs := parseNDJSON(t, out)
	if len(recs) != 1 || recs[0]["path"] != "nested.md" {
		t.Fatalf("path arg scoping wrong: %v", recs)
	}
}

func TestLoreMetaCommand_UnknownFlagErrors(t *testing.T) {
	_, errOut, code := runMeta(t, metaTreeFS(t), "/docs", "lore meta --json")
	if code != 1 {
		t.Fatalf("unknown flag exit=%d, want 1", code)
	}
	if !strings.Contains(errOut, "unknown flag") {
		t.Fatalf("stderr missing flag error: %s", errOut)
	}
}

func TestLore_UsageListsMeta(t *testing.T) {
	out, _, _ := runMeta(t, metaTreeFS(t), "/", "lore")
	if !strings.Contains(out, "meta") || !strings.Contains(out, "docsets") {
		t.Fatalf("lore usage should list meta and docsets:\n%s", out)
	}
}
