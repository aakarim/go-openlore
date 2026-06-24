package passkeys

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	tokenLength = 32
	tokenTTL    = 5 * time.Minute
)

// PendingRegistration represents an in-flight passkey registration.
type PendingRegistration struct {
	Token     string
	Lore      string
	Name      string
	UserID    []byte
	Session   *webauthn.SessionData
	ExpiresAt time.Time
}

// PendingStore holds pending registrations in memory with automatic expiry.
type PendingStore struct {
	mu      sync.Mutex
	pending map[string]*PendingRegistration
	stopCh  chan struct{}
}

// NewPendingStore creates a new in-memory pending registration store.
func NewPendingStore() *PendingStore {
	ps := &PendingStore{
		pending: make(map[string]*PendingRegistration),
		stopCh:  make(chan struct{}),
	}
	go ps.cleanup()
	return ps
}

// Create generates a new pending registration and returns it.
func (ps *PendingStore) Create(loreName, passkeyName string) (*PendingRegistration, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	userID, err := GenerateUserID()
	if err != nil {
		return nil, err
	}

	pr := &PendingRegistration{
		Token:     token,
		Lore:      loreName,
		Name:      passkeyName,
		UserID:    userID,
		ExpiresAt: time.Now().Add(tokenTTL),
	}

	ps.mu.Lock()
	ps.pending[token] = pr
	ps.mu.Unlock()

	return pr, nil
}

// Get retrieves a pending registration by token. Returns nil if not found or expired.
func (ps *PendingStore) Get(token string) *PendingRegistration {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	pr, ok := ps.pending[token]
	if !ok {
		return nil
	}
	if time.Now().After(pr.ExpiresAt) {
		delete(ps.pending, token)
		return nil
	}
	return pr
}

// Delete removes a pending registration.
func (ps *PendingStore) Delete(token string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.pending, token)
}

// Stop shuts down the cleanup goroutine.
func (ps *PendingStore) Stop() {
	close(ps.stopCh)
}

func (ps *PendingStore) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			ps.mu.Lock()
			for token, pr := range ps.pending {
				if now.After(pr.ExpiresAt) {
					delete(ps.pending, token)
				}
			}
			ps.mu.Unlock()
		case <-ps.stopCh:
			return
		}
	}
}

func generateToken() (string, error) {
	b := make([]byte, tokenLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
