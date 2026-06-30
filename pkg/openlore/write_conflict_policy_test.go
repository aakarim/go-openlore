package openlore

import (
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// Default (hash) policy: sed -i is a true compare-and-swap. If the file drifts
// from the base sed read, the commit is rejected instead of clobbering.
func TestSedInPlace_HashPolicy_RejectsDrift(t *testing.T) {
	sh, d, _ := newWritableShell(t)
	if _, _, code := run(sh, "echo original > /doc.md"); code != 0 {
		t.Fatal("seed write failed")
	}

	// Simulate a concurrent change by replacing the file's base content with a
	// known stale value, then attempt a CAS commit against the stale base.
	stale := []byte("stale-base\n")
	if _, err := d.WriteFileAtomic("/doc.md", []byte("someone-elses-edit\n"), vfs.WriteOpts{}); err != nil {
		t.Fatalf("concurrent write: %v", err)
	}
	// WriteFileCAS with a base that no longer matches => precondition failure.
	if _, err := cmds.WriteFileCAS(sh, "/doc.md", []byte("my-transform\n"), stale); err == nil {
		t.Fatal("CAS against a drifted base must fail")
	}
	if got, _ := d.ReadFile("/doc.md"); string(got) != "someone-elses-edit\n" {
		t.Fatalf("drifted file must not be clobbered, got %q", got)
	}
}

// last_write_wins policy: an overwrite commits unconditionally even when the
// base differs.
func TestOverwrite_LastWriteWins_NoPrecondition(t *testing.T) {
	sh, d, _ := newWritableShell(t)
	sh.SetConflictPolicyFn(func(string) vfs.WriteConflictPolicy { return vfs.PolicyLastWriteWins })

	if _, _, code := run(sh, "echo one > /x.md"); code != 0 {
		t.Fatal("seed failed")
	}
	// Concurrent change.
	if _, err := d.WriteFileAtomic("/x.md", []byte("two\n"), vfs.WriteOpts{}); err != nil {
		t.Fatalf("concurrent: %v", err)
	}
	if _, errs, code := run(sh, "echo three > /x.md"); code != 0 {
		t.Fatalf("last_write_wins overwrite should succeed: %q", errs)
	}
	if got, _ := d.ReadFile("/x.md"); string(got) != "three\n" {
		t.Fatalf("last write should win, got %q", got)
	}
}

// Server resolves the global policy and per-docset overrides for a path.
func TestServer_WriteConflictPolicyResolution(t *testing.T) {
	s := &Server{
		config: config.Config{WriteConflictPolicy: vfs.PolicyHash},
		auth: &config.AuthConfig{
			Docsets: map[string]config.DocsetSpec{
				"loose": {
					Paths:               []config.PathMapping{{Source: "/scratch"}},
					WriteConflictPolicy: string(vfs.PolicyLastWriteWins),
				},
				"strict": {
					Paths: []config.PathMapping{{Source: "/knowledge"}},
					// no override => inherits global (hash)
				},
			},
		},
	}

	cases := []struct {
		path string
		want vfs.WriteConflictPolicy
	}{
		{"/scratch/note.md", vfs.PolicyLastWriteWins}, // per-docset override
		{"/scratch", vfs.PolicyLastWriteWins},         // docset root itself
		{"/knowledge/topic.md", vfs.PolicyHash},       // inherits global
		{"/elsewhere/file.md", vfs.PolicyHash},        // no docset => global
	}
	for _, c := range cases {
		if got := s.writeConflictPolicy(c.path); got != c.want {
			t.Errorf("writeConflictPolicy(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// A nil resolver yields the default hash policy.
func TestShell_DefaultConflictPolicyIsHash(t *testing.T) {
	sh := shell.NewShell(nil)
	if got := sh.WriteConflictPolicy("/anything"); got != vfs.PolicyHash {
		t.Fatalf("default policy = %q, want hash", got)
	}
}

func TestParseWriteConflictPolicy(t *testing.T) {
	if p, err := vfs.ParseWriteConflictPolicy(""); err != nil || p != vfs.PolicyHash {
		t.Fatalf("empty => %q,%v want hash", p, err)
	}
	if p, err := vfs.ParseWriteConflictPolicy("last_write_wins"); err != nil || p != vfs.PolicyLastWriteWins {
		t.Fatalf("last_write_wins => %q,%v", p, err)
	}
	if _, err := vfs.ParseWriteConflictPolicy("bogus"); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("bogus should error, got %v", err)
	}
}
