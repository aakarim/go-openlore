package openlore

import (
	"context"
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
)

// contextWithIdentity + identityFromContext are the single identity-resolution
// path shared by SSH and MCP/HTTP. A stored identity must round-trip unchanged;
// an empty context must fall back to the anonymous identity.
func TestIdentityFromContext_RoundTrip(t *testing.T) {
	s := &Server{merge: NewMergeFS()}

	want := Identity{IdentityName: "frontend", LoreName: "frontend", Scopes: []string{ScopeFull}}
	ctx := contextWithIdentity(context.Background(), want)

	got := s.identityFromContext(ctx)
	if got.IdentityName != "frontend" || got.LoreName != "frontend" {
		t.Fatalf("identity did not round-trip: %+v", got)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != ScopeFull {
		t.Fatalf("scopes did not round-trip: %v", got.Scopes)
	}
}

// An anonymous caller under an auth config lands on the default lore and holds
// NO scope — fail-closed, never full.
func TestIdentityFromContext_AnonymousIsNotFull(t *testing.T) {
	s := &Server{
		merge:        NewMergeFS(),
		authEnforced: true,
		auth: &config.AuthConfig{
			Lore: map[string][]string{"default": {"public"}},
		},
	}

	got := s.identityFromContext(context.Background())
	if got.LoreName != "default" {
		t.Fatalf("anonymous caller should resolve to default lore, got %q", got.LoreName)
	}
	if len(got.Scopes) != 0 {
		t.Fatalf("anonymous caller must not carry any scope (fail-closed), got %v", got.Scopes)
	}
}

// With no auth config at all, the anonymous caller has full access and carries
// the full scope sentinel.
func TestIdentityFromContext_NoAuthIsFull(t *testing.T) {
	merge := NewMergeFS()
	merge.Mount("public", NewFSAdapter(nil))
	// Unenforced mode: NewServer synthesizes a `public` docset at "/" so every
	// consumer reuses the normal docset machinery. auth is always non-nil.
	s := &Server{
		merge: merge,
		auth: &config.AuthConfig{
			Docsets: map[string]config.DocsetSpec{
				"public": {Paths: []config.PathMapping{{Source: "/", Display: "/"}}},
			},
			Lore: map[string][]string{"default": {"public"}},
		},
	}

	got := s.identityFromContext(context.Background())
	if len(got.Scopes) != 1 || got.Scopes[0] != ScopeFull {
		t.Fatalf("no-auth caller should hold full scope, got %v", got.Scopes)
	}
}
