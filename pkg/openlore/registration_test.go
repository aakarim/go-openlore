package openlore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postJSON(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, registrationPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.registrationHandler(rec, req)
	return rec
}

// TestRegistration_ClaudeLikeClientSucceeds registers a remote-HTTPS client (as
// Claude Desktop would) and verifies a public PKCE client is issued with no
// secret and persisted for later /authorize redirect validation.
func TestRegistration_ClaudeLikeClientSucceeds(t *testing.T) {
	s := newTokenTestServer(t, true, "allow")
	rec := postJSON(t, s, `{
		"client_name": "Claude",
		"redirect_uris": ["https://claude.ai/api/mcp/auth_callback"]
	}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := doc["client_secret"]; ok {
		t.Error("public client must not receive a client_secret")
	}
	if doc["token_endpoint_auth_method"] != "none" {
		t.Errorf("token_endpoint_auth_method = %v, want none", doc["token_endpoint_auth_method"])
	}
	clientID, _ := doc["client_id"].(string)
	if clientID == "" {
		t.Fatal("no client_id issued")
	}
	// Persisted and looked up.
	client, ok, err := s.clientStore.Lookup(context.Background(), clientID)
	if err != nil || !ok {
		t.Fatalf("Lookup(%q) = (_, %v, %v), want registered", clientID, ok, err)
	}
	if !client.AllowsRedirect("https://claude.ai/api/mcp/auth_callback") {
		t.Error("registered redirect not stored")
	}
}

func TestRegistration_RejectsBadInput(t *testing.T) {
	s := newTokenTestServer(t, true, "allow")
	cases := []string{
		`{}`, // no redirect_uris
		`{"redirect_uris": ["http://example.com/cb"]}`,                                                     // remote http (not loopback)
		`{"redirect_uris": ["https://x.example/cb#frag"]}`,                                                 // fragment
		`{"redirect_uris": ["https://x.example/cb"], "token_endpoint_auth_method": "client_secret_basic"}`, // confidential
		`{"redirect_uris": ["https://x.example/cb"], "grant_types": ["password"]}`,                         // unsupported grant
		`not json`,
	}
	for i, body := range cases {
		rec := postJSON(t, s, body)
		if rec.Code < 400 {
			t.Errorf("case %d (%s): status = %d, want >=400", i, body, rec.Code)
		}
	}
}

// TestRegistration_ThenAuthorizeAllowsRemoteRedirect proves the DCR → authorize
// binding: an unregistered remote redirect is rejected, but the same redirect is
// accepted once registered under its client_id.
func TestRegistration_ThenAuthorizeAllowsRemoteRedirect(t *testing.T) {
	s := newTokenTestServer(t, true, "allow")
	remote := "https://claude.ai/cb"

	// Unregistered remote redirect → rejected.
	if s.validAuthorizeRedirect(context.Background(), "", remote) {
		t.Fatal("unregistered remote redirect should be rejected")
	}

	rec := postJSON(t, s, `{"redirect_uris": ["`+remote+`"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d", rec.Code)
	}
	var doc map[string]any
	json.Unmarshal(rec.Body.Bytes(), &doc)
	clientID := doc["client_id"].(string)

	// Registered client + exact redirect → accepted.
	if !s.validAuthorizeRedirect(context.Background(), clientID, remote) {
		t.Error("registered exact redirect should be accepted")
	}
	// Registered client + different redirect → rejected (exact match only).
	if s.validAuthorizeRedirect(context.Background(), clientID, "https://claude.ai/other") {
		t.Error("mismatched redirect should be rejected")
	}
}
