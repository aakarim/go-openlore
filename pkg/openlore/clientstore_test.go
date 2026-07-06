package openlore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileClientStore_SaveLookupReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth", "clients.json")

	store, err := newFileClientStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	client := OAuthClient{
		ClientID:                "olc_test",
		RedirectURIs:            []string{"https://claude.ai/cb"},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code"},
		ResponseTypes:           []string{"code"},
		Scope:                   ScopeFull,
		ClientIDIssuedAt:        time.Now().Truncate(time.Second),
	}
	if err := store.Save(context.Background(), client); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Reload from disk → client persists.
	reloaded, err := newFileClientStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok, err := reloaded.Lookup(context.Background(), "olc_test")
	if err != nil || !ok {
		t.Fatalf("lookup after reload = (_, %v, %v), want found", ok, err)
	}
	if !got.AllowsRedirect("https://claude.ai/cb") {
		t.Error("reloaded client lost redirect")
	}
}

func TestFileClientStore_MissingFileIsEmpty(t *testing.T) {
	store, err := newFileClientStore(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, ok, _ := store.Lookup(context.Background(), "anything"); ok {
		t.Error("empty store returned a client")
	}
}

func TestFileClientStore_MalformedFileErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clients.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newFileClientStore(path); err == nil {
		t.Error("malformed store file should error, not start empty")
	}
}
