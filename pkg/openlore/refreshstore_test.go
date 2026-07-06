package openlore

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func testRefreshStore(t *testing.T) *fileRefreshStore {
	t.Helper()
	rs, err := newFileRefreshStore(filepath.Join(t.TempDir(), "refresh.json"))
	if err != nil {
		t.Fatalf("newFileRefreshStore: %v", err)
	}
	return rs
}

func TestRefreshStore_SaveLookup(t *testing.T) {
	rs := testRefreshStore(t)
	rt := RefreshToken{Token: "a", Subject: "alice", ChainID: "c1", ExpiresAt: time.Now().Add(time.Hour)}
	if err := rs.Save(rt); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok, err := rs.Lookup("a")
	if err != nil || !ok {
		t.Fatalf("Lookup: ok=%v err=%v", ok, err)
	}
	if got.Subject != "alice" {
		t.Errorf("subject = %q", got.Subject)
	}
}

func TestRefreshStore_RotateIssuesNewAndConsumesOld(t *testing.T) {
	rs := testRefreshStore(t)
	old := RefreshToken{Token: "old", Subject: "alice", ChainID: "c1", ExpiresAt: time.Now().Add(time.Hour)}
	rs.Save(old)

	next := RefreshToken{Token: "new", Subject: "alice", ChainID: "c1", ExpiresAt: time.Now().Add(time.Hour)}
	if err := rs.Rotate("old", next); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	// New token is present and unused.
	if got, ok, _ := rs.Lookup("new"); !ok || got.Used {
		t.Fatalf("new token missing or already used: ok=%v", ok)
	}
	// Old token is now marked used.
	if got, ok, _ := rs.Lookup("old"); !ok || !got.Used {
		t.Fatalf("old token should be marked used: ok=%v used=%v", ok, got.Used)
	}
}

func TestRefreshStore_ReuseRevokesChain(t *testing.T) {
	rs := testRefreshStore(t)
	// A chain of two rotated tokens.
	rs.Save(RefreshToken{Token: "old", Subject: "alice", ChainID: "c1", ExpiresAt: time.Now().Add(time.Hour)})
	rs.Rotate("old", RefreshToken{Token: "new", Subject: "alice", ChainID: "c1", ExpiresAt: time.Now().Add(time.Hour)})

	// Re-presenting the already-used "old" is theft → revoke whole chain.
	err := rs.Rotate("old", RefreshToken{Token: "attacker", Subject: "alice", ChainID: "c1", ExpiresAt: time.Now().Add(time.Hour)})
	if !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("expected ErrRefreshReuse, got %v", err)
	}
	// The entire chain is revoked: the previously-valid "new" is gone.
	if _, ok, _ := rs.Lookup("new"); ok {
		t.Fatalf("reuse must revoke the whole chain; 'new' still present")
	}
	if _, ok, _ := rs.Lookup("attacker"); ok {
		t.Fatalf("attacker token must not be stored")
	}
}

func TestRefreshStore_RotateUnknownInvalid(t *testing.T) {
	rs := testRefreshStore(t)
	err := rs.Rotate("nope", RefreshToken{Token: "x", ChainID: "c1"})
	if !errors.Is(err, ErrRefreshInvalid) {
		t.Fatalf("expected ErrRefreshInvalid, got %v", err)
	}
}

func TestRefreshStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "refresh.json")
	rs, _ := newFileRefreshStore(path)
	rs.Save(RefreshToken{Token: "a", Subject: "alice", ChainID: "c1", ExpiresAt: time.Now().Add(time.Hour)})

	// A fresh store reading the same file sees the token.
	rs2, err := newFileRefreshStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, ok, _ := rs2.Lookup("a"); !ok {
		t.Fatalf("token did not persist across reopen")
	}
}
