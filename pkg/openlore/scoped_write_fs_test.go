package openlore

import (
	"errors"
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// rootsAuthz builds a writeAuthorizer that permits writes strictly inside any of
// the given display roots — the classic per-identity write-isolation behavior.
func rootsAuthz(roots ...string) writeAuthorizer {
	return func(_ vfs.ChangeAction, p string) bool {
		clean := vfs.CleanPath(p)
		for _, r := range roots {
			r = vfs.CleanPath(r)
			if r == "/" {
				if clean != "/" {
					return true
				}
				continue
			}
			if strings.HasPrefix(clean, r+"/") {
				return true
			}
		}
		return false
	}
}

// Part B per-identity write isolation. Two agents share a lore (so they can see
// the same docsets), but each session is scoped to the docset roots it may
// publish to — so neither can write the other's docset, and neither can write
// the read-only root.
func TestScopedWriteFS_ConfinesWritesToRoots(t *testing.T) {
	dir := t.TempDir()
	base := NewDirFS(dir, config.FilesConfig{})
	if err := base.SetWriteable(); err != nil {
		t.Fatalf("SetWriteable: %v", err)
	}
	// Pre-create the docset roots so writes land somewhere real.
	for _, d := range []string{"/jared", "/claw"} {
		if err := base.Mkdir(d); err != nil {
			t.Fatalf("seed mkdir %s: %v", d, err)
		}
	}

	// This session may only write /jared.
	fs := newScopedWriteFS(base, rootsAuthz("/jared"))

	if _, err := fs.WriteFileAtomic("/jared/notes.md", []byte("ok"), vfs.WriteOpts{}); err != nil {
		t.Fatalf("write inside own docset should succeed: %v", err)
	}
	if _, err := fs.WriteFileAtomic("/claw/notes.md", []byte("nope"), vfs.WriteOpts{}); !errors.Is(err, vfs.ErrReadOnly) {
		t.Fatalf("write to another agent's docset should be read-only, got %v", err)
	}
	if _, err := fs.WriteFileAtomic("/root.md", []byte("nope"), vfs.WriteOpts{}); !errors.Is(err, vfs.ErrReadOnly) {
		t.Fatalf("write to root should be read-only, got %v", err)
	}
	// Mkdir is gated the same way.
	if err := fs.Mkdir("/claw/sub"); !errors.Is(err, vfs.ErrReadOnly) {
		t.Fatalf("mkdir in another agent's docset should be read-only, got %v", err)
	}
	if err := fs.Mkdir("/jared/sub"); err != nil {
		t.Fatalf("mkdir in own docset should succeed: %v", err)
	}
}

// An identity with no writable roots (anonymous / unrecognized) gets a fully
// read-only view even though the substrate itself is writable.
func TestScopedWriteFS_NoRoots_FullyReadOnly(t *testing.T) {
	dir := t.TempDir()
	base := NewDirFS(dir, config.FilesConfig{})
	if err := base.SetWriteable(); err != nil {
		t.Fatalf("SetWriteable: %v", err)
	}
	if err := base.Mkdir("/jared"); err != nil {
		t.Fatalf("seed mkdir: %v", err)
	}

	fs := newScopedWriteFS(base, nil)
	if _, err := fs.WriteFileAtomic("/jared/x.md", []byte("nope"), vfs.WriteOpts{}); !errors.Is(err, vfs.ErrReadOnly) {
		t.Fatalf("no-roots session must be read-only everywhere, got %v", err)
	}
}

// Reads always pass through, regardless of write scope.
func TestScopedWriteFS_ReadsPassThrough(t *testing.T) {
	dir := t.TempDir()
	base := NewDirFS(dir, config.FilesConfig{})
	if err := base.SetWriteable(); err != nil {
		t.Fatalf("SetWriteable: %v", err)
	}
	if err := base.Mkdir("/claw"); err != nil {
		t.Fatalf("seed mkdir: %v", err)
	}
	if _, err := base.WriteFileAtomic("/claw/readme.md", []byte("hi"), vfs.WriteOpts{}); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	// Session scoped to /jared can still read /claw.
	fs := newScopedWriteFS(base, rootsAuthz("/jared"))
	data, err := fs.ReadFile("/claw/readme.md")
	if err != nil {
		t.Fatalf("read should pass through scope: %v", err)
	}
	if string(data) != "hi" {
		t.Fatalf("read = %q, want %q", data, "hi")
	}
}
