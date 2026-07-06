package openlore

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aakarim/go-openlore/internal/config"
)

// newTokenTestServer builds a Server with token auth enabled: two docsets
// (public in the default lore, secret only in eng), an identity "alice" in eng,
// and an ES256 issuer keyed under a temp DataDir.
func newTokenTestServer(t *testing.T, allowKeyless bool, unknownIdentity string) *Server {
	t.Helper()

	merge := NewMergeFS()
	merge.Mount("public", NewFSAdapter(fstest.MapFS{"hello.txt": {Data: []byte("public\n")}}))
	merge.Mount("secret", NewFSAdapter(fstest.MapFS{"top.txt": {Data: []byte("secret\n")}}))

	keyless := allowKeyless
	s := &Server{
		merge:        merge,
		authEnforced: true,
		auth: &config.AuthConfig{
			AllowKeyless:    &keyless,
			UnknownIdentity: unknownIdentity,
			Docsets: map[string]config.DocsetSpec{
				"public": {Paths: []config.PathMapping{{Source: "/public", Display: "/public"}}},
				"secret": {Paths: []config.PathMapping{{Source: "/secret", Display: "/secret"}}},
			},
			Lore: map[string][]string{
				"default": {"public"},
				"eng":     {"public", "secret"},
			},
			Identities: []config.AuthIdentity{
				{Name: "alice", Lore: "eng"},
			},
		},
		config: config.Config{
			DataDir:         t.TempDir(),
			AllowKeyless:    allowKeyless,
			UnknownIdentity: unknownIdentity,
			// Token config is server infrastructure (openlore.yml), not lore.json.
			Tokens: &config.AuthTokensConfig{
				Issuer:   "https://openlore.test",
				Audience: "https://openlore.test",
			},
		},
	}
	if err := s.initAuth(); err != nil {
		t.Fatalf("initAuth: %v", err)
	}
	return s
}

// identityEcho is a handler that reports the resolved identity from context.
func (s *Server) identityEcho() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := s.identityFromContext(r.Context())
		name := id.IdentityName
		if name == "" {
			name = "anonymous"
		}
		w.Write([]byte(name + " " + id.LoreName))
	})
}

func mint(t *testing.T, s *Server, sub, scope string) string {
	t.Helper()
	tok, _, err := s.issuer.Mint(sub, scope, 30*time.Minute)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	return tok
}

func TestAuthMiddleware_KeylessNoTokenIsAnonymous(t *testing.T) {
	s := newTokenTestServer(t, true, "allow")
	h := s.authMiddleware(s.identityEcho())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "anonymous default" {
		t.Fatalf("body = %q, want %q", got, "anonymous default")
	}
}

func TestAuthMiddleware_ValidTokenResolvesIdentity(t *testing.T) {
	s := newTokenTestServer(t, true, "allow")
	h := s.authMiddleware(s.identityEcho())

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+mint(t, s, "alice", ScopeFull))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "alice eng" {
		t.Fatalf("body = %q, want %q", got, "alice eng")
	}
}

func TestAuthMiddleware_InvalidTokenRejectedEvenKeyless(t *testing.T) {
	s := newTokenTestServer(t, true, "allow")
	h := s.authMiddleware(s.identityEcho())

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddleware_RequiredPostureRejectsMissingToken(t *testing.T) {
	s := newTokenTestServer(t, false, "deny")
	h := s.authMiddleware(s.identityEcho())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/mcp", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	challenge := rec.Header().Get("WWW-Authenticate")
	if challenge == "" {
		t.Fatalf("expected WWW-Authenticate challenge header")
	}
	// The challenge must point OAuth-native clients at the resource metadata so
	// they can discover the flow (RFC 9728 §5.1).
	if !strings.Contains(challenge, `resource_metadata="`+s.resourceMetadataURL()+`"`) {
		t.Errorf("challenge = %q, want resource_metadata pointing at %q", challenge, s.resourceMetadataURL())
	}
}

func TestAuthMiddleware_UnknownSubDenyIs403(t *testing.T) {
	s := newTokenTestServer(t, false, "deny")
	h := s.authMiddleware(s.identityEcho())

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+mint(t, s, "nobody", ScopeFull))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthMiddleware_AnonymousTokenResolvesToDefault(t *testing.T) {
	s := newTokenTestServer(t, true, "allow")
	h := s.authMiddleware(s.identityEcho())

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+mint(t, s, anonymousSubject, ScopeFull))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Body.String(); got != "anonymous default" {
		t.Fatalf("public token body = %q, want %q", got, "anonymous default")
	}
}
