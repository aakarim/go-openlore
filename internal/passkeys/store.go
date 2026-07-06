package passkeys

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// StoredCredential wraps a webauthn.Credential with metadata.
type StoredCredential struct {
	// UserID is the WebAuthn user handle (random bytes, base64url-encoded in JSON).
	UserID []byte `json:"user_id"`
	// Name is a human-readable device label for this passkey.
	Name string `json:"name"`
	// Identity is the OpenLore identity name this passkey authenticates as. It
	// becomes the token `sub` at login, from which authority (lore, capabilities,
	// home) is resolved live via the identity table (docs/mcp-bearer-auth.md §7).
	Identity string `json:"identity"`
	// CreatedAt is when the passkey was registered.
	CreatedAt time.Time `json:"created_at"`
	// Credential is the WebAuthn credential data.
	Credential webauthn.Credential `json:"credential"`
}

// StoreData is the on-disk JSON format.
type StoreData struct {
	Credentials []StoredCredential `json:"credentials"`
}

// Store manages passkey credentials on disk as a JSON file.
type Store struct {
	mu   sync.RWMutex
	path string
	data StoreData
}

// NewStore creates or loads a passkey store from the given file path.
func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.data = StoreData{}
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &s.data)
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// Add persists a new credential.
func (s *Store) Add(cred StoredCredential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Credentials = append(s.data.Credentials, cred)
	return s.save()
}

// FindByCredentialID returns the stored credential matching the given WebAuthn credential ID.
func (s *Store) FindByCredentialID(credID []byte) (*StoredCredential, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.data.Credentials {
		if bytesEqual(s.data.Credentials[i].Credential.ID, credID) {
			return &s.data.Credentials[i], true
		}
	}
	return nil, false
}

// FindByUserID returns the stored credential matching the given user handle.
func (s *Store) FindByUserID(userID []byte) (*StoredCredential, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := range s.data.Credentials {
		if bytesEqual(s.data.Credentials[i].UserID, userID) {
			return &s.data.Credentials[i], true
		}
	}
	return nil, false
}

// AllCredentials returns all stored credentials.
func (s *Store) AllCredentials() []StoredCredential {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]StoredCredential, len(s.data.Credentials))
	copy(out, s.data.Credentials)
	return out
}

// Remove deletes a credential by name. Returns true if found and removed.
func (s *Store) Remove(name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.data.Credentials {
		if c.Name == name {
			s.data.Credentials = append(s.data.Credentials[:i], s.data.Credentials[i+1:]...)
			return true, s.save()
		}
	}
	return false, nil
}

// UpdateSignCount updates the sign count for a credential after successful auth.
func (s *Store) UpdateSignCount(credID []byte, newCount uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Credentials {
		if bytesEqual(s.data.Credentials[i].Credential.ID, credID) {
			s.data.Credentials[i].Credential.Authenticator.SignCount = newCount
			return s.save()
		}
	}
	return nil
}

// GenerateUserID creates a random 32-byte user handle.
func GenerateUserID() ([]byte, error) {
	id := make([]byte, 32)
	_, err := rand.Read(id)
	return id, err
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
