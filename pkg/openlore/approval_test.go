package openlore

import (
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

func newTestRequest(t *testing.T, store *RequestStore, target string, base, proposed []byte) ApprovalRequest {
	t.Helper()
	req := ApprovalRequest{
		TargetPath:         target,
		Action:             "publish",
		ProposerIdentity:   "claude",
		RequiredCapability: "approve@oncall",
		BaseExists:         base != nil,
		ProposedHash:       hashOf(proposed),
	}
	if base != nil {
		req.BaseHash = hashOf(base)
	}
	got, err := store.Create(req, base, proposed)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return got
}

func TestRequestStore_CreateGetListProposed(t *testing.T) {
	store, err := NewRequestStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewRequestStore: %v", err)
	}

	r1 := newTestRequest(t, store, "/ops/freeze", []byte("old\n"), []byte("new\n"))
	if r1.ID == "" || r1.Status != RequestPending {
		t.Fatalf("Create returned bad request: %+v", r1)
	}

	got, err := store.Get(r1.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TargetPath != "/ops/freeze" || !got.BaseExists {
		t.Fatalf("Get mismatch: %+v", got)
	}

	proposed, err := store.Proposed(r1.ID)
	if err != nil || string(proposed) != "new\n" {
		t.Fatalf("Proposed = %q err=%v", proposed, err)
	}
	base, err := store.Base(r1.ID)
	if err != nil || string(base) != "old\n" {
		t.Fatalf("Base = %q err=%v", base, err)
	}

	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %d err=%v", len(list), err)
	}
}

func TestRequestStore_UpdateTransition(t *testing.T) {
	store, _ := NewRequestStore(t.TempDir())
	r := newTestRequest(t, store, "/ops/freeze", nil, []byte("freeze\n"))
	r.Status = RequestApproved
	r.ApprovedBy = "alice"
	if err := store.Update(r); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := store.Get(r.ID)
	if got.Status != RequestApproved || got.ApprovedBy != "alice" {
		t.Fatalf("transition not persisted: %+v", got)
	}
}

func TestRequestsFS_ReadOnlyAndRenders(t *testing.T) {
	store, _ := NewRequestStore(t.TempDir())
	r := newTestRequest(t, store, "/ops/freeze", []byte("old\n"), []byte("new\n"))
	rfs := NewRequestsFS(store)

	// Not writable: writes to /requests must be impossible.
	if _, ok := interface{}(rfs).(vfs.WritableFS); ok {
		t.Fatal("RequestsFS must not implement WritableFS")
	}

	// Directory lists the request.
	entries, err := rfs.ReadDir("/")
	if err != nil || len(entries) != 1 || entries[0].FileName != r.ID {
		t.Fatalf("ReadDir = %+v err=%v", entries, err)
	}

	// File renders status + diff.
	content, err := rfs.ReadFile("/" + r.ID)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	s := string(content)
	for _, want := range []string{"status:   PENDING", "target:   /ops/freeze", "- old", "+ new"} {
		if !strings.Contains(s, want) {
			t.Fatalf("render missing %q in:\n%s", want, s)
		}
	}
}

// /requests is a system mount: it must survive FilteredView so every session
// can see it regardless of its lore docsets.
func TestRequestsFS_SystemMountSurvivesFilter(t *testing.T) {
	store, _ := NewRequestStore(t.TempDir())
	r := newTestRequest(t, store, "/ops/freeze", nil, []byte("x\n"))

	m := NewMergeFS()
	m.MountSystem("requests", NewRequestsFS(store))
	m.Mount("jared", NewDirFS(t.TempDir(), config.FilesConfig{}))

	// A session whose lore is only {jared} still sees /requests.
	view := m.FilteredView(map[string]bool{"jared": true})
	if _, err := view.Stat("/requests/" + r.ID); err != nil {
		t.Fatalf("/requests should survive FilteredView: %v", err)
	}

	// And it is read-only through the merge: a write to /requests is denied
	// because RequestsFS is not a WritableFS.
	if _, err := view.WriteFileAtomic("/requests/"+r.ID, []byte("nope"), vfs.WriteOpts{}); err == nil {
		t.Fatal("writing /requests must be denied")
	}
}
