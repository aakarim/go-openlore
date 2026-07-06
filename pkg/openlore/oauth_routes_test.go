package openlore

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// mountedOAuthServer wires oauthRoutes onto a real ServeMux behind httptest —
// the same mounting Start() does — so the routing (path constants, /authorize vs
// /authorize/public separation, well-known paths) is exercised end to end.
func mountedOAuthServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s := newTokenTestServer(t, true, "allow")
	mux := http.NewServeMux()
	for path, h := range s.oauthRoutes() {
		mux.Handle(path, h)
	}
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return s, ts
}

// TestOAuthRoutes_DiscoveryToTokenRoundTrip drives the exact sequence an
// OAuth-native MCP client follows: discover metadata → register (DCR) →
// authorize (public choice) → exchange the code for a token, all over HTTP.
func TestOAuthRoutes_DiscoveryToTokenRoundTrip(t *testing.T) {
	_, ts := mountedOAuthServer(t)

	// 1. Discover protected-resource + AS metadata.
	prm := getJSON(t, ts.URL+protectedResourceMetadataPath)
	if prm["resource"] == "" || prm["resource"] == nil {
		t.Fatalf("protected-resource-metadata missing resource: %v", prm)
	}
	asm := getJSON(t, ts.URL+authServerMetadataPath)
	regEndpoint, _ := asm["registration_endpoint"].(string)
	authEndpoint, _ := asm["authorization_endpoint"].(string)
	tokEndpoint, _ := asm["token_endpoint"].(string)
	if regEndpoint == "" || authEndpoint == "" || tokEndpoint == "" {
		t.Fatalf("AS metadata missing endpoints: %v", asm)
	}

	// 2. Register a Claude-like client (remote HTTPS callback via loopback here
	//    so the httptest redirect target is reachable by the test).
	redirectURI := "http://127.0.0.1:52321/callback"
	regResp := postJSONReq(t, ts.URL+registrationPath, `{"client_name":"Claude","redirect_uris":["`+redirectURI+`"]}`)
	clientID, _ := regResp["client_id"].(string)
	if clientID == "" {
		t.Fatalf("DCR returned no client_id: %v", regResp)
	}

	// 3. GET /authorize (choice page) with the registered client + PKCE.
	verifier, challenge := pkcePair()
	authzURL := ts.URL + authorizePath + "?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {"st8"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode()
	body := getString(t, authzURL)
	m := regexp.MustCompile(`name="authz" value="([^"]+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no authz id on choice page: %s", body)
	}
	authz := m[1]

	// 4. POST /authorize/public → 302 back to redirect_uri?code=&state=.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.PostForm(ts.URL+authorizePublicPath, url.Values{"authz": {authz}})
	if err != nil {
		t.Fatalf("public post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("public choice status = %d, want 302", resp.StatusCode)
	}
	loc, _ := url.Parse(resp.Header.Get("Location"))
	if loc.Query().Get("state") != "st8" {
		t.Errorf("state = %q, want st8", loc.Query().Get("state"))
	}
	code := loc.Query().Get("code")
	if code == "" {
		t.Fatal("no code in redirect")
	}

	// 5. Exchange the code (with client_id + PKCE verifier) for a token.
	tokResp, err := client.PostForm(ts.URL+tokenPath, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {clientID},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	})
	if err != nil {
		t.Fatalf("token post: %v", err)
	}
	defer tokResp.Body.Close()
	if tokResp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(tokResp.Body)
		t.Fatalf("token status = %d, want 200; body=%s", tokResp.StatusCode, b)
	}
	var tr tokenResponse
	json.NewDecoder(tokResp.Body).Decode(&tr)
	if tr.AccessToken == "" {
		t.Fatal("no access_token")
	}
}

// TestOAuthRoutes_TokenExchangeRejectsWrongClientID proves the code↔client
// binding survives the real mux.
func TestOAuthRoutes_TokenExchangeRejectsWrongClientID(t *testing.T) {
	s, ts := mountedOAuthServer(t)
	verifier, challenge := pkcePair()
	redirectURI := "http://127.0.0.1:52322/cb"
	regResp := postJSONReq(t, ts.URL+registrationPath, `{"redirect_uris":["`+redirectURI+`"]}`)
	clientID := regResp["client_id"].(string)

	body := getString(t, ts.URL+authorizePath+"?"+url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}.Encode())
	authz := regexp.MustCompile(`name="authz" value="([^"]+)"`).FindStringSubmatch(body)[1]
	redirectURL, ok := s.CompleteAuthorize(authz, anonymousSubject)
	if !ok {
		t.Fatal("CompleteAuthorize failed")
	}
	u, _ := url.Parse(redirectURL)
	code := u.Query().Get("code")

	client := &http.Client{}
	resp, err := client.PostForm(ts.URL+tokenPath, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {"olc_someone_else"},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	})
	if err != nil {
		t.Fatalf("token post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("expected rejection for mismatched client_id")
	}
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d", url, resp.StatusCode)
	}
	var doc map[string]any
	json.NewDecoder(resp.Body).Decode(&doc)
	return doc
}

func getString(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func postJSONReq(t *testing.T, url, body string) map[string]any {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	var doc map[string]any
	json.NewDecoder(resp.Body).Decode(&doc)
	return doc
}
