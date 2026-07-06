package openlore

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode metadata: %v; body=%s", err, rec.Body.String())
	}
	return doc
}

func TestProtectedResourceMetadata(t *testing.T) {
	s := newTokenTestServer(t, true, "allow")
	rec := httptest.NewRecorder()
	s.resourceMetadata(rec, httptest.NewRequest(http.MethodGet, protectedResourceMetadataPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	doc := decodeJSON(t, rec)
	// resource MUST equal the audience tokens are minted for (Q4).
	if doc["resource"] != s.config.Tokens.Audience {
		t.Errorf("resource = %v, want %q", doc["resource"], s.config.Tokens.Audience)
	}
	servers, _ := doc["authorization_servers"].([]any)
	if len(servers) != 1 || servers[0] != s.issuerBaseURL() {
		t.Errorf("authorization_servers = %v, want [%q]", doc["authorization_servers"], s.issuerBaseURL())
	}
}

func TestAuthServerMetadata(t *testing.T) {
	s := newTokenTestServer(t, true, "allow")
	rec := httptest.NewRecorder()
	s.authServerMetadata(rec, httptest.NewRequest(http.MethodGet, authServerMetadataPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	doc := decodeJSON(t, rec)
	base := s.issuerBaseURL()
	want := map[string]string{
		"issuer":                 base,
		"authorization_endpoint": base + authorizePath,
		"token_endpoint":         base + tokenPath,
		"registration_endpoint":  base + registrationPath,
		"jwks_uri":               base + jwksPath,
	}
	for k, v := range want {
		if doc[k] != v {
			t.Errorf("%s = %v, want %q", k, doc[k], v)
		}
	}
	// MCP clients refuse to proceed without code_challenge_methods_supported.
	methods, _ := doc["code_challenge_methods_supported"].([]any)
	if len(methods) != 1 || methods[0] != "S256" {
		t.Errorf("code_challenge_methods_supported = %v, want [S256]", doc["code_challenge_methods_supported"])
	}
	// jwt-bearer is a Phase 4 stub — must not be advertised.
	grants, _ := doc["grant_types_supported"].([]any)
	for _, g := range grants {
		if strings.Contains(g.(string), "jwt-bearer") {
			t.Errorf("grant_types_supported advertises jwt-bearer: %v", grants)
		}
	}
}
