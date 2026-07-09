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

// TestValidateGrants_RejectsDuplicateDisplayRoots pins that two docsets sharing
// the same display root are rejected at startup (ambiguous access boundary).
func TestValidateGrants_RejectsDuplicateDisplayRoots(t *testing.T) {
	s := &Server{
		authEnforced: true,
		grants:       newGrantRegistry(),
		auth: &config.AuthConfig{
			Docsets: map[string]config.DocsetSpec{
				"a": {Paths: []config.PathMapping{{Source: "/x", Display: "/x"}}},
				"b": {Paths: []config.PathMapping{{Source: "/other", Display: "/x"}}},
			},
		},
	}
	if err := s.validateGrants(); err == nil {
		t.Fatal("expected validateGrants to reject two docsets sharing display root /x")
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

// TestGrantForPath_NestedDocsetOverridesAncestor pins that a grant on an
// ancestor docset (the root "/") does not reach into a nested docset the
// identity lacks a grant on: /alfie is carved out of a root "openlore" grant.
func TestGrantForPath_NestedDocsetOverridesAncestor(t *testing.T) {
	s := grantTestServer()
	// Add a root docset "/" alongside the existing /alfie and /miles docsets.
	s.auth.Docsets["openlore"] = config.DocsetSpec{Paths: []config.PathMapping{{Source: "/", Display: "/"}}}

	// Identity holds rw on the root docset only — no grant on /alfie.
	id := Identity{IdentityName: "anon", Grants: map[string]string{"openlore": "rw"}, Scopes: []string{ScopeFull}}

	// A root-level file is governed by the root docset → writable.
	if !s.identityCanWrite(id, vfs.ChangeActionWrite, "/readme.md") {
		t.Fatal("root grant should write a root-level file")
	}
	// /alfie is a more-specific docset that carves itself out → denied despite
	// the root rw grant.
	if s.identityCanWrite(id, vfs.ChangeActionWrite, "/alfie/x.md") {
		t.Fatal("nested docset must override ancestor grant: /alfie write should be denied")
	}
}

// TestScopedReadFS_NestedDocsetCarveOut pins that an identity with read access to
// the root "/" cannot read or list a nested docset it lacks a grant on.
func TestScopedReadFS_NestedDocsetCarveOut(t *testing.T) {
	merge := NewMergeFS()
	merge.SetRoot(NewFSAdapter(fstest.MapFS{
		"readme.md":       {Data: []byte("root")},
		"alfie/secret.md": {Data: []byte("alfie")},
		"miles/secret.md": {Data: []byte("miles")},
	}))
	// Readable: the root "/" only. Boundaries: root + the two nested docsets.
	fs := newScopedReadFS(merge, []string{"/"}, []string{"/", "/alfie", "/miles"})

	// Root-level file is readable.
	if _, err := fs.ReadFile("/readme.md"); err != nil {
		t.Fatalf("root-level file should be readable: %v", err)
	}
	// Nested docsets are carved out even though "/" is readable.
	if _, err := fs.ReadFile("/alfie/secret.md"); err == nil {
		t.Fatal("nested docset /alfie must be carved out of the root grant")
	}
	if _, err := fs.ReadFile("/miles/secret.md"); err == nil {
		t.Fatal("nested docset /miles must be carved out of the root grant")
	}

	// Listing "/" shows the root file but hides the carved-out nested docsets.
	entries, err := fs.ReadDir("/")
	if err != nil {
		t.Fatalf("ReadDir(/): %v", err)
	}
	names := map[string]bool{}
	for _, e := range entries {
		names[e.FileName] = true
	}
	if !names["readme.md"] {
		t.Fatalf("root listing should show root-level files: %+v", entries)
	}
	if names["alfie"] || names["miles"] {
		t.Fatalf("root listing must hide carved-out nested docsets: %+v", entries)
	}
}

func TestScopedReadFS_HidesSiblingDocsets(t *testing.T) {
	merge := NewMergeFS()
	merge.SetRoot(NewFSAdapter(fstest.MapFS{
		"alfie/secret.md": {Data: []byte("alfie")},
		"miles/secret.md": {Data: []byte("miles")},
	}))
	// Session may read only /alfie.
	fs := newScopedReadFS(merge, []string{"/alfie"}, []string{"/alfie", "/miles"})

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
