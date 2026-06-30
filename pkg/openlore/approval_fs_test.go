package openlore

import (
	"errors"
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// layered builds the production write stack for a session: a writable DirFS
// substrate wrapped by approvalFS (gating) wrapped by scopedWriteFS (scope), so
// tests exercise the same ordering the server wires.
func layered(t *testing.T, writableRoots []string, gated map[string]string) (vfs.WritableFS, *RequestStore, *DirFS) {
	t.Helper()
	base := NewDirFS(t.TempDir(), config.FilesConfig{})
	if err := base.SetWriteable(); err != nil {
		t.Fatalf("SetWriteable: %v", err)
	}
	store, err := NewRequestStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewRequestStore: %v", err)
	}
	decide := func(p string) (string, bool) {
		c, ok := gated[vfs.CleanPath(p)]
		return c, ok
	}
	af := newApprovalFS(base, store, decide, "claude", nil)
	return newScopedWriteFS(af, writableRoots), store, base
}

func TestApprovalFS_GatedWriteCreatesPendingWithoutMutating(t *testing.T) {
	fs, store, base := layered(t, []string{"/ops"}, map[string]string{"/ops/freeze": "approve@oncall"})
	if err := base.Mkdir("/ops"); err != nil {
		t.Fatalf("seed mkdir: %v", err)
	}

	_, err := fs.WriteFileAtomic("/ops/freeze", []byte("ON"), vfs.WriteOpts{})
	var pe *vfs.PendingApprovalError
	if !errors.As(err, &pe) {
		t.Fatalf("want PendingApprovalError, got %v", err)
	}
	if pe.Capability != "approve@oncall" || pe.Target != "/ops/freeze" {
		t.Fatalf("bad pending error: %+v", pe)
	}

	// Target must NOT have been committed.
	if _, err := base.ReadFile("/ops/freeze"); err == nil {
		t.Fatal("gated write must not commit the target")
	}

	// Exactly one PENDING request recorded, with the proposed bytes.
	list, _ := store.List()
	if len(list) != 1 || list[0].Status != RequestPending {
		t.Fatalf("want 1 PENDING request, got %+v", list)
	}
	if list[0].ID != pe.RequestID || list[0].BaseExists {
		t.Fatalf("request mismatch: %+v", list[0])
	}
	proposed, _ := store.Proposed(pe.RequestID)
	if string(proposed) != "ON" {
		t.Fatalf("proposed = %q, want ON", proposed)
	}
}

func TestApprovalFS_NonGatedWriteCommits(t *testing.T) {
	fs, store, base := layered(t, []string{"/ops"}, map[string]string{"/ops/freeze": "approve@oncall"})
	if err := base.Mkdir("/ops"); err != nil {
		t.Fatalf("seed mkdir: %v", err)
	}
	if _, err := fs.WriteFileAtomic("/ops/notes.md", []byte("hi"), vfs.WriteOpts{}); err != nil {
		t.Fatalf("non-gated write should commit: %v", err)
	}
	if b, err := base.ReadFile("/ops/notes.md"); err != nil || string(b) != "hi" {
		t.Fatalf("non-gated write not committed: %q err=%v", b, err)
	}
	if list, _ := store.List(); len(list) != 0 {
		t.Fatalf("non-gated write must not create a request, got %d", len(list))
	}
}

// scopedWriteFS must hard-deny an out-of-scope write before approvalFS can turn
// it into a pending request.
func TestApprovalFS_ScopeDenialWinsOverApproval(t *testing.T) {
	// Gated path is /ops/freeze but the session may only write /jared.
	fs, store, _ := layered(t, []string{"/jared"}, map[string]string{"/ops/freeze": "approve@oncall"})
	_, err := fs.WriteFileAtomic("/ops/freeze", []byte("ON"), vfs.WriteOpts{})
	if !errors.Is(err, vfs.ErrReadOnly) {
		t.Fatalf("out-of-scope gated write must be read-only, got %v", err)
	}
	if list, _ := store.List(); len(list) != 0 {
		t.Fatalf("scope denial must not create a request, got %d", len(list))
	}
}

// A stale precondition fails immediately without parking a doomed request.
func TestApprovalFS_StalePreconditionNoRequest(t *testing.T) {
	fs, store, base := layered(t, []string{"/ops"}, map[string]string{"/ops/freeze": "approve@oncall"})
	if err := base.Mkdir("/ops"); err != nil {
		t.Fatalf("seed mkdir: %v", err)
	}
	if _, err := base.WriteFileAtomic("/ops/freeze", []byte("v1"), vfs.WriteOpts{}); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	wrong := "deadbeef"
	_, err := fs.WriteFileAtomic("/ops/freeze", []byte("v2"), vfs.WriteOpts{IfMatch: &wrong})
	var pre *vfs.PreconditionError
	if !errors.As(err, &pre) {
		t.Fatalf("stale IfMatch should fail with PreconditionError, got %v", err)
	}
	if list, _ := store.List(); len(list) != 0 {
		t.Fatalf("stale write must not create a request, got %d", len(list))
	}
}

func TestApprovalPathMatch(t *testing.T) {
	cases := []struct {
		pattern, p string
		want       bool
	}{
		{"/ops/freeze", "/ops/freeze", true},
		{"/ops/freeze", "/ops/freeze.md", false},
		{"/ops/deploy/**", "/ops/deploy", true},
		{"/ops/deploy/**", "/ops/deploy/worker.yaml", true},
		{"/ops/deploy/**", "/ops/deployx", false},
		{"/ops/*.yaml", "/ops/a.yaml", true},
		{"/ops/*.yaml", "/ops/sub/a.yaml", false},
	}
	for _, c := range cases {
		if got := approvalPathMatch(c.pattern, c.p); got != c.want {
			t.Errorf("approvalPathMatch(%q,%q)=%v want %v", c.pattern, c.p, got, c.want)
		}
	}
}
