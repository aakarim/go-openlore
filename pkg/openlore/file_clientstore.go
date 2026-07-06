package openlore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// fileClientStore is a mutex-guarded JSON-file ClientStore, keyed by client_id.
type fileClientStore struct {
	mu      sync.Mutex
	path    string
	clients map[string]OAuthClient
}

func newFileClientStore(path string) (*fileClientStore, error) {
	s := &fileClientStore{path: path, clients: map[string]OAuthClient{}}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &s.clients); err != nil {
			return nil, fmt.Errorf("parsing client store %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading client store %s: %w", path, err)
	}
	return s, nil
}

// persist writes the current map to disk. Caller must hold the mutex.
func (s *fileClientStore) persist() error {
	if s.path == "" {
		return nil
	}
	b, err := json.Marshal(s.clients)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *fileClientStore) Save(_ context.Context, client OAuthClient) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[client.ClientID] = client
	return s.persist()
}

func (s *fileClientStore) Lookup(_ context.Context, clientID string) (OAuthClient, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.clients[clientID]
	return c, ok, nil
}
