package passkeys

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const sessionCookieName = "openlore_session"

// SessionManager handles HMAC-signed session cookies.
type SessionManager struct {
	key []byte
	ttl time.Duration
}

// NewSessionManager creates a session manager keyed from the given secret.
func NewSessionManager(key []byte, ttl time.Duration) *SessionManager {
	// Derive a session-specific key so raw host key material isn't used directly.
	h := sha256.Sum256(append([]byte("openlore-passkey-session:"), key...))
	return &SessionManager{key: h[:], ttl: ttl}
}

// SessionInfo holds the decoded session values.
type SessionInfo struct {
	Lore      string
	ExpiresAt time.Time
}

// SetCookie creates and sets a signed session cookie on the response.
func (sm *SessionManager) SetCookie(w http.ResponseWriter, lore string) {
	expiry := time.Now().Add(sm.ttl)
	payload := fmt.Sprintf("%s:%d", lore, expiry.Unix())
	sig := sm.sign(payload)
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(sig)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  expiry,
	})
}

// ValidateRequest checks the session cookie and returns session info if valid.
func (sm *SessionManager) ValidateRequest(r *http.Request) (*SessionInfo, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, false
	}

	parts := strings.SplitN(cookie.Value, ".", 2)
	if len(parts) != 2 {
		return nil, false
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}

	payload := string(payloadBytes)
	if !sm.verify(payload, sigBytes) {
		return nil, false
	}

	sepIdx := strings.LastIndex(payload, ":")
	if sepIdx < 0 {
		return nil, false
	}

	lore := payload[:sepIdx]
	expiryUnix, err := strconv.ParseInt(payload[sepIdx+1:], 10, 64)
	if err != nil {
		return nil, false
	}

	expiry := time.Unix(expiryUnix, 0)
	if time.Now().After(expiry) {
		return nil, false
	}

	return &SessionInfo{Lore: lore, ExpiresAt: expiry}, true
}

// ClearCookie removes the session cookie.
func (sm *SessionManager) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func (sm *SessionManager) sign(payload string) []byte {
	mac := hmac.New(sha256.New, sm.key)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func (sm *SessionManager) verify(payload string, sig []byte) bool {
	expected := sm.sign(payload)
	return hmac.Equal(expected, sig)
}
