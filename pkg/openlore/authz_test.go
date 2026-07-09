package openlore

import (
	"testing"
	"testing/fstest"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// grantTestServer builds an auth-enforced, writable server with two path-based
// docsets sharing one root filesystem (alfie, miles), each with an /inbox, plus
// the inbox plugin registered so the `publish` grant resolves.
func grantTestServer() *Server {
	s := &Server{
		merge:        NewMergeFS(),
		authEnforced: true,
		grants:       newGrantRegistry(),
		config:       config.Config{Readonly: false},
		auth: &config.AuthConfig{
			Docsets: map[string]config.DocsetSpec{
				"alfie": {Paths: []config.PathMapping{{Source: "/alfie", Display: "/alfie"}}, Inbox: "inbox"},
				"miles": {Paths: []config.PathMapping{{Source: "/miles", Display: "/miles"}}, Inbox: "inbox"},
			},
		},
	}
	s.registerPlugin(NewInboxPlugin())
	return s
}

func TestIdentityCanWrite_RW(t *testing.T) {
	s := grantTestServer()
	id := Identity{IdentityName: "alfie", Grants: map[string]string{"alfie": "rw"}, Scopes: []string{ScopeFull}}

	if !s.identityCanWrite(id, vfs.ChangeActionWrite, "/alfie/notes.md") {
		t.Fatal("rw should write anywhere in its docset")
	}
	if !s.identityCanWrite(id, vfs.ChangeActionRemove, "/alfie/notes.md") {
		t.Fatal("rw should delete in its docset")
	}
	// No grant on miles → denied.
	if s.identityCanWrite(id, vfs.ChangeActionWrite, "/miles/notes.md") {
		t.Fatal("no grant on miles → write must be denied")
	}
}

func TestIdentityCanWrite_RO(t *testing.T) {
	s := grantTestServer()
	id := Identity{IdentityName: "bob", Grants: map[string]string{"alfie": "ro"}, Scopes: []string{ScopeFull}}
	if s.identityCanWrite(id, vfs.ChangeActionWrite, "/alfie/notes.md") {
		t.Fatal("ro must never write")
	}
}

func TestIdentityCanWrite_Publish(t *testing.T) {
	s := grantTestServer()
	// miles holds publish on alfie: create/edit only within /alfie/inbox, no deletes.
	id := Identity{IdentityName: "miles", Grants: map[string]string{"alfie": "publish"}, Scopes: []string{ScopeFull}}

	if !s.identityCanWrite(id, vfs.ChangeActionWrite, "/alfie/inbox/from-miles.md") {
		t.Fatal("publish should write inside the inbox")
	}
	if !s.identityCanWrite(id, vfs.ChangeActionMkdir, "/alfie/inbox/sub") {
		t.Fatal("publish should mkdir inside the inbox")
	}
	if s.identityCanWrite(id, vfs.ChangeActionWrite, "/alfie/notes.md") {
		t.Fatal("publish must NOT write outside the inbox")
	}
	if s.identityCanWrite(id, vfs.ChangeActionRemove, "/alfie/inbox/from-miles.md") {
		t.Fatal("publish must NOT delete even inside the inbox")
	}
	if s.identityCanWrite(id, vfs.ChangeActionRemoveAll, "/alfie/inbox") {
		t.Fatal("publish must NOT remove_all")
	}
}

func TestIdentityCanWrite_ScopeAndLockCeilings(t *testing.T) {
	s := grantTestServer()
	id := Identity{IdentityName: "alfie", Grants: map[string]string{"alfie": "rw"}, Scopes: []string{ScopeRead}}
	if s.identityCanWrite(id, vfs.ChangeActionWrite, "/alfie/notes.md") {
		t.Fatal("a read-scoped token must not write even with an rw grant")
	}

	full := Identity{IdentityName: "alfie", Grants: map[string]string{"alfie": "rw"}, Scopes: []string{ScopeFull}}
	s.config.Readonly = true
	if s.identityCanWrite(full, vfs.ChangeActionWrite, "/alfie/notes.md") {
		t.Fatal("the global write lock must block all writes")
	}
}

func TestValidateGrants(t *testing.T) {
	// publish referenced but inbox plugin NOT registered → fail-closed.
	s := &Server{
		authEnforced: true,
		grants:       newGrantRegistry(), // only ro/rw
		auth: &config.AuthConfig{
			Docsets:    map[string]config.DocsetSpec{"alfie": {}},
			Identities: []config.AuthIdentity{{Name: "miles", Docsets: map[string]string{"alfie": "publish"}}},
		},
	}
	if err := s.validateGrants(); err == nil {
		t.Fatal("expected validateGrants to reject unregistered 'publish' grant")
	}

	// Register the inbox plugin → publish becomes valid.
	s.registerPlugin(NewInboxPlugin())
	if err := s.validateGrants(); err != nil {
		t.Fatalf("publish should validate once the inbox plugin is registered: %v", err)
	}
}

// TestSessionPublishTargets_ResolvesInboxPath pins that a publish target
// carries the docset's resolved inbox path (not just its name), so `publish`
// routes into /docset/inbox/... where the publish grant actually permits writes.
func TestSessionPublishTargets_ResolvesInboxPath(t *testing.T) {
	s := grantTestServer()
	id := Identity{
		IdentityName: "miles",
		Grants:       map[string]string{"alfie": "publish"},
		Scopes:       []string{ScopeFull},
	}
	targets := s.sessionPublishTargets(id)
	if len(targets) != 1 {
		t.Fatalf("want 1 publish target, got %d: %+v", len(targets), targets)
	}
	if targets[0].Name != "alfie" {
		t.Fatalf("target name = %q, want alfie", targets[0].Name)
	}
	if targets[0].InboxPath != "/alfie/inbox" {
		t.Fatalf("target inbox path = %q, want /alfie/inbox", targets[0].InboxPath)
	}
}

func TestScopedReadFS_HidesSiblingDocsets(t *testing.T) {
	merge := NewMergeFS()
	merge.SetRoot(NewFSAdapter(fstest.MapFS{
		"alfie/secret.md": {Data: []byte("alfie")},
		"miles/secret.md": {Data: []byte("miles")},
	}))
	// Session may read only /alfie.
	fs := newScopedReadFS(merge, []string{"/alfie"})

	if _, err := fs.ReadFile("/alfie/secret.md"); err != nil {
		t.Fatalf("should read granted docset: %v", err)
	}
	if _, err := fs.ReadFile("/miles/secret.md"); err == nil {
		t.Fatal("must not read ungranted sibling docset")
	}

	// Root listing shows only /alfie, never /miles.
	entries, err := fs.ReadDir("/")
	if err != nil {
		t.Fatalf("ReadDir(/): %v", err)
	}
	for _, e := range entries {
		if e.FileName == "miles" {
			t.Fatalf("root listing must hide ungranted docset: %+v", entries)
		}
	}
	var sawAlfie bool
	for _, e := range entries {
		if e.FileName == "alfie" {
			sawAlfie = true
		}
	}
	if !sawAlfie {
		t.Fatalf("root listing should show the granted docset: %+v", entries)
	}
}
