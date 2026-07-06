package openlore

import (
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
)

// Step 1 of Part C: `publish` no longer writes straight to disk — it commits
// through the session VFS, so it inherits per-identity write scoping (and, in
// later steps, CAS + approval gating). These tests pin that behavior: a publish
// to an in-scope docset commits and is readable back through the same FS, and a
// publish to a docset the session may *see* but not *write* is denied without
// touching disk.
func TestPublish_GoesThroughScopedVFS(t *testing.T) {
	dir := t.TempDir()
	base := NewDirFS(dir, config.FilesConfig{})
	if err := base.SetWriteable(); err != nil {
		t.Fatalf("SetWriteable: %v", err)
	}
	for _, d := range []string{"/jared", "/claw"} {
		if err := base.Mkdir(d); err != nil {
			t.Fatalf("seed mkdir %s: %v", d, err)
		}
	}

	// Session can see both docsets but may only write /jared.
	fs := newScopedWriteFS(base, []string{"/jared"})
	sh := shell.NewShell(fs)
	sh.SetPublishTargets([]cmds.PublishTarget{{Name: "jared"}, {Name: "claw"}})

	// In-scope publish commits and reads back through the VFS.
	if out, errs, code := run(sh, "echo hello | publish /jared/topic.md"); code != 0 {
		t.Fatalf("in-scope publish failed: code=%d out=%q err=%q", code, out, errs)
	}
	if out, _, _ := run(sh, "cat /jared/topic.md"); out != "hello\n" {
		t.Fatalf("published content = %q, want %q", out, "hello\n")
	}

	// Out-of-scope publish is denied by the scoped VFS, not committed.
	out, errs, code := run(sh, "echo nope | publish /claw/topic.md")
	if code == 0 {
		t.Fatalf("out-of-scope publish should fail; out=%q", out)
	}
	if !strings.Contains(errs, "no access") {
		t.Fatalf("want 'no access' error, got %q", errs)
	}
	if _, _, c := run(sh, "cat /claw/topic.md"); c == 0 {
		t.Fatalf("out-of-scope publish must not write /claw/topic.md")
	}
}
