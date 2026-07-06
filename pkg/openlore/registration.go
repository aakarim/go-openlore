package openlore

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// maxRegistrationBody caps the DCR request body — open registration must not let
// a caller stream an unbounded body into memory.
const maxRegistrationBody = 16 << 10 // 16 KiB

// clientRegistrationRequest is the subset of RFC 7591 client metadata OpenLore
// honors. Everything else is ignored; unsupported values are rejected.
type clientRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
}

// registrationHandler serves POST /register (RFC 7591 Dynamic Client
// Registration). Registration is open so MCP clients (Claude Desktop, Cowork)
// self-register; OpenLore only issues public PKCE clients (no client_secret).
// The registered redirect_uris are what let a remote HTTPS client be trusted at
// /authorize (docs/mcp-bearer-auth.md §11 Phase 3).
func (s *Server) registrationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		oauthError(w, http.StatusMethodNotAllowed, "invalid_request", "POST required")
		return
	}
	var req clientRegistrationRequest
	body := io.LimitReader(r.Body, maxRegistrationBody)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}

	if len(req.RedirectURIs) == 0 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "at least one redirect_uri required")
		return
	}
	for _, uri := range req.RedirectURIs {
		if !validRegisteredRedirectURI(uri) {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "unsupported redirect_uri: "+uri)
			return
		}
	}

	// Public PKCE clients only. Reject confidential auth methods.
	authMethod := req.TokenEndpointAuthMethod
	if authMethod == "" {
		authMethod = "none"
	}
	if authMethod != "none" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata",
			"only token_endpoint_auth_method=none (public client) is supported")
		return
	}

	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}
	for _, g := range grantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported grant_type: "+g)
			return
		}
	}

	responseTypes := req.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}
	for _, rt := range responseTypes {
		if rt != "code" {
			oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported response_type: "+rt)
			return
		}
	}

	scope := req.Scope
	if scope == "" {
		scope = ScopeFull
	}

	client := OAuthClient{
		ClientID:                "olc_" + randomToken(),
		ClientName:              req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		TokenEndpointAuthMethod: "none",
		GrantTypes:              grantTypes,
		ResponseTypes:           responseTypes,
		Scope:                   scope,
		ClientIDIssuedAt:        time.Now(),
	}
	if err := s.clientStore.Save(r.Context(), client); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "could not persist client")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"client_id":                  client.ClientID,
		"client_id_issued_at":        client.ClientIDIssuedAt.Unix(),
		"client_name":                client.ClientName,
		"redirect_uris":              client.RedirectURIs,
		"token_endpoint_auth_method": client.TokenEndpointAuthMethod,
		"grant_types":                client.GrantTypes,
		"response_types":             client.ResponseTypes,
		"scope":                      client.Scope,
	})
}
