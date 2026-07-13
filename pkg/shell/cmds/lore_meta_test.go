package cmds_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/pkg/meta"
	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
)

// The `lore meta` command lives here (shell plumbing); its scanning business
// logic lives in pkg/meta and is tested there. These tests exercise the command
// adapter: arg parsing, cwd scoping, NDJSON emission, and extender wiring.

func metaTreeFS() *mapFS {
	fs := newMapFS()
	fs.AddFile("/docs/orders.md", "---\ntype: Table\ntitle: Orders\ntags:\n  - sales\n  - orders\n---\n# Orders\nbody\n")
	fs.AddFile("/docs/metric.md", "---\ntype: Metric\ntitle: Revenue\n---\nbody\n")
	fs.AddFile("/docs/plain.md", "# No frontmatter here\njust text\n")
	fs.AddFile("/docs/notes.txt", "---\ntype: NotMarkdown\n---\n")
	fs.AddFile("/docs/sub/nested.md", "---\ntype: Note\n---\nnested body\n")
	return fs
}

func runMeta(t *testing.T, sh *shell.Shell, cwd, cmd string) (string, string, int) {
	t.Helper()
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
	out, errOut, code := runMeta(t, shell.NewShell(metaTreeFS()), "/docs", "lore meta")
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
	out, _, code := runMeta(t, shell.NewShell(metaTreeFS()), "/", "lore meta docs/sub")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	recs := parseNDJSON(t, out)
	if len(recs) != 1 || recs[0]["path"] != "nested.md" {
		t.Fatalf("path arg scoping wrong: %v", recs)
	}
}

func TestLoreMetaCommand_UnknownFlagErrors(t *testing.T) {
	_, errOut, code := runMeta(t, shell.NewShell(metaTreeFS()), "/docs", "lore meta --json")
	if code != 1 {
		t.Fatalf("unknown flag exit=%d, want 1", code)
	}
	if !strings.Contains(errOut, "unknown flag") {
		t.Fatalf("stderr missing flag error: %s", errOut)
	}
}

func TestLoreMetaCommand_AppliesSessionExtenders(t *testing.T) {
	sh := shell.NewShell(metaTreeFS())
	sh.SetMetaExtenders([]meta.Extender{
		func(absPath string, content []byte, fm map[string]any) map[string]any {
			if fm["type"] == "Metric" {
				return map[string]any{"annotated": true}
			}
			return nil
		},
	})
	out, errOut, code := runMeta(t, sh, "/docs", "lore meta")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errOut)
	}
	for _, r := range parseNDJSON(t, out) {
		if r["path"] == "metric.md" && r["annotated"] != true {
			t.Fatalf("session extender not applied to metric.md: %v", r)
		}
		if r["path"] != "metric.md" {
			if _, ok := r["annotated"]; ok {
				t.Fatalf("extender leaked onto %v", r["path"])
			}
		}
	}
}

func TestLore_UsageListsMeta(t *testing.T) {
	out, _, _ := runMeta(t, shell.NewShell(metaTreeFS()), "/", "lore")
	if !strings.Contains(out, "meta") || !strings.Contains(out, "docsets") {
		t.Fatalf("lore usage should list meta and docsets:\n%s", out)
	}
}

// TestRegisterLoreSub_PluginCanAddSubcommand verifies the generic dispatcher
// lets a plugin register a new subcommand.
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

	sh := shell.NewShell(testFS())
	var out, errOut bytes.Buffer
	code := sh.ExecPipeline("lore ping", &out, &errOut, nil)
	if code != 0 {
		t.Fatalf("registered subcommand exit=%d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "pong") {
		t.Fatalf("subcommand output = %q, want pong", out.String())
	}
}
