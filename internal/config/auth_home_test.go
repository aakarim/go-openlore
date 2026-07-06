package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeAuth(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "lore.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}
	return p
}

func TestLoadAuthConfigHomeValid(t *testing.T) {
	p := writeAuth(t, `{
		"docsets": {
			"public": {"paths": ["/docs/public"]},
			"agent-home": {"paths": [{"published/agent": "/home/agent"}]}
		},
		"lore": {"agent": ["public", "agent-home"]},
		"identities": [
			{"name": "a1", "public_key": "ssh-ed25519 AAAA", "lore": "agent", "home": "agent-home"}
		]
	}`)

	auth, err := LoadAuthConfig(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := auth.Identities[0].Home; got != "agent-home" {
		t.Errorf("home: got %q, want %q", got, "agent-home")
	}
}

func TestLoadAuthConfigHomeNotInLore(t *testing.T) {
	p := writeAuth(t, `{
		"docsets": {
			"public": {"paths": ["/docs/public"]},
			"other": {"paths": ["/docs/other"]}
		},
		"lore": {"agent": ["public"]},
		"identities": [
			{"name": "a1", "public_key": "ssh-ed25519 AAAA", "lore": "agent", "home": "other"}
		]
	}`)

	if _, err := LoadAuthConfig(p); err == nil {
		t.Fatal("expected error for home docset not in lore, got nil")
	}
}
