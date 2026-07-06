package cmds_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
)

// runLore executes a command line against a shell seeded with the given docsets.
func runLore(t *testing.T, docsets []cmds.DocsetInfo, cmd string) (string, string, int) {
	t.Helper()
	sh := shell.NewShell(testFS())
	sh.SetDocsets(docsets)
	var out, errOut bytes.Buffer
	code := sh.ExecPipeline(cmd, &out, &errOut, nil)
	return out.String(), errOut.String(), code
}

func TestLore_BareShowsUsageExitsZero(t *testing.T) {
	out, _, code := runLore(t, nil, "lore")
	if code != 0 {
		t.Fatalf("bare lore exit = %d, want 0", code)
	}
	if !strings.Contains(out, "Usage: lore <command>") || !strings.Contains(out, "docsets") {
		t.Fatalf("bare lore output missing usage/subcommand:\n%s", out)
	}
}

func TestLore_UnknownSubcommandExitsOne(t *testing.T) {
	out, errOut, code := runLore(t, nil, "lore bogus")
	if code != 1 {
		t.Fatalf("unknown subcommand exit = %d, want 1", code)
	}
	if out != "" {
		t.Fatalf("unknown subcommand should not write stdout, got %q", out)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Fatalf("stderr missing error:\n%s", errOut)
	}
}

func TestLoreDocsets_Table(t *testing.T) {
	docsets := []cmds.DocsetInfo{
		{Name: "public", Paths: []string{"/docs/public", "/docs/getting-started.md"}},
		{Name: "backend", Paths: []string{"/docs/backend", "/docs/api"}, Writable: true, Approval: true},
		{Name: "home", Paths: []string{"/home/backend"}, Writable: true, Home: true, HasPublish: true},
	}
	out, _, code := runLore(t, docsets, "lore docsets")
	if code != 0 {
		t.Fatalf("lore docsets exit = %d, want 0", code)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want header + 3 rows, got %d lines:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "DOCSET") {
		t.Fatalf("first line should be header, got %q", lines[0])
	}
	// Fields are grep/field-splittable.
	assertRow := func(line, name, access, attrs, paths string) {
		t.Helper()
		f := strings.Fields(line)
		if f[0] != name || f[1] != access || f[2] != attrs {
			t.Fatalf("row %q: got name=%q access=%q attrs=%q; want %q %q %q", line, f[0], f[1], f[2], name, access, attrs)
		}
		if !strings.HasSuffix(strings.TrimRight(line, " "), paths) {
			t.Fatalf("row %q: paths should end with %q", line, paths)
		}
	}
	assertRow(lines[1], "public", "r", "-", "/docs/public,/docs/getting-started.md")
	assertRow(lines[2], "backend", "rw", "approval", "/docs/backend,/docs/api")
	assertRow(lines[3], "home", "rw", "home,publish", "/home/backend")
}

func TestLoreDocsets_EmptyShowsHeaderOnly(t *testing.T) {
	out, _, code := runLore(t, nil, "lore docsets")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if strings.TrimRight(out, "\n") != "DOCSET  ACCESS  ATTRIBUTES  PATHS" {
		t.Fatalf("empty docsets should print header only, got:\n%q", out)
	}
}

func TestLoreDocsets_GrepByAttribute(t *testing.T) {
	docsets := []cmds.DocsetInfo{
		{Name: "public", Paths: []string{"/docs"}},
		{Name: "backend", Paths: []string{"/docs/backend"}, Writable: true, HasPublish: true},
	}
	out, _, code := runLore(t, docsets, "lore docsets | grep publish")
	if code != 0 {
		t.Fatalf("piped grep exit = %d, want 0", code)
	}
	if !strings.Contains(out, "backend") || strings.Contains(out, "public ") {
		t.Fatalf("grep publish should return only the backend row, got:\n%s", out)
	}
}
