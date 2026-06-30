package openlore

import (
	"strings"
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// approveFlowFixture wires the full Part C stack the way the server does: a
// writable substrate, a request store, the approval backend (committing through
// the raw substrate), and a proposer shell whose FS is approvalFS-inside-
// scopedWriteFS. It returns the proposer shell and the substrate so the test
// can drive a propose→approve cycle and assert the committed bytes.
func approveFlowFixture(t *testing.T, gated map[string]string) (proposer *shell.Shell, store *RequestStore, base *DirFS) {
	t.Helper()
	base = NewDirFS(t.TempDir(), config.FilesConfig{})
	if err := base.SetWriteable(); err != nil {
		t.Fatalf("SetWriteable: %v", err)
	}
	if err := base.Mkdir("/ops"); err != nil {
		t.Fatalf("seed mkdir: %v", err)
	}
	var err error
	store, err = NewRequestStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewRequestStore: %v", err)
	}

	saved := cmds.Approvals
	cmds.Approvals = &approvalBackend{store: store, commitFS: base}
	t.Cleanup(func() { cmds.Approvals = saved })

	decide := func(p string) (string, bool) {
		c, ok := gated[p]
		return c, ok
	}
	af := newApprovalFS(base, store, decide, "claude", nil)
	scoped := newScopedWriteFS(af, []string{"/ops"})
	proposer = shell.NewShell(scoped)
	proposer.SetEnv("OPENLORE_DOCSETS", "ops")
	return proposer, store, base
}

// approverShell builds a session for a human holding the given capabilities.
func approverShell(name string, caps []string) *shell.Shell {
	sh := shell.NewShell(nil)
	sh.SetAllowedActions([]cmds.Action{cmds.ActionApprove})
	sh.SetEnv("OPENLORE_IDENTITY", name)
	if len(caps) > 0 {
		sh.SetEnv("OPENLORE_CAPABILITIES", strings.Join(caps, ","))
	}
	return sh
}

func onlyRequestID(t *testing.T, store *RequestStore) string {
	t.Helper()
	list, _ := store.List()
	if len(list) != 1 {
		t.Fatalf("want exactly 1 request, got %d", len(list))
	}
	return list[0].ID
}

func TestApproveFlow_ProposeThenApproveCommits(t *testing.T) {
	proposer, store, base := approveFlowFixture(t, map[string]string{"/ops/freeze": "approve@oncall"})

	// Propose: a redirect to the gated path becomes pending (exit 0, no commit).
	if _, errs, code := run(proposer, "echo ON > /ops/freeze"); code != 0 || !strings.Contains(errs, "pending approval") {
		t.Fatalf("propose: code=%d err=%q", code, errs)
	}
	if _, err := base.ReadFile("/ops/freeze"); err == nil {
		t.Fatal("target must not be committed while pending")
	}
	id := onlyRequestID(t, store)

	// Approver with the capability commits it through CAS.
	alice := approverShell("alice", []string{"approve@oncall"})
	out, errs, code := run(alice, "approve "+id)
	if code != 0 {
		t.Fatalf("approve failed: code=%d out=%q err=%q", code, out, errs)
	}
	if !strings.Contains(out, "Approved") {
		t.Fatalf("approve output = %q", out)
	}
	if b, err := base.ReadFile("/ops/freeze"); err != nil || string(b) != "ON\n" {
		t.Fatalf("approved write not committed: %q err=%v", b, err)
	}
	got, _ := store.Get(id)
	if got.Status != RequestApproved || got.ApprovedBy != "alice" {
		t.Fatalf("request not marked approved: %+v", got)
	}
}

func TestApproveFlow_DeniedWithoutCapability(t *testing.T) {
	proposer, store, base := approveFlowFixture(t, map[string]string{"/ops/freeze": "approve@oncall"})
	run(proposer, "echo ON > /ops/freeze")
	id := onlyRequestID(t, store)

	// Bob lacks approve@oncall.
	bob := approverShell("bob", []string{"approve@something-else"})
	_, errs, code := run(bob, "approve "+id)
	if code == 0 {
		t.Fatal("approve without capability must fail")
	}
	if !strings.Contains(errs, "denied") {
		t.Fatalf("want denial message, got %q", errs)
	}
	if _, err := base.ReadFile("/ops/freeze"); err == nil {
		t.Fatal("denied approval must not commit")
	}
	if got, _ := store.Get(id); got.Status != RequestPending {
		t.Fatalf("denied request should stay PENDING, got %s", got.Status)
	}
}

func TestApproveFlow_Reject(t *testing.T) {
	proposer, store, base := approveFlowFixture(t, map[string]string{"/ops/freeze": "approve@oncall"})
	run(proposer, "echo ON > /ops/freeze")
	id := onlyRequestID(t, store)

	alice := approverShell("alice", []string{"approve@oncall"})
	if out, _, code := run(alice, "reject "+id); code != 0 || !strings.Contains(out, "Rejected") {
		t.Fatalf("reject failed: code=%d out=%q", code, out)
	}
	if _, err := base.ReadFile("/ops/freeze"); err == nil {
		t.Fatal("rejected request must not commit")
	}
	if got, _ := store.Get(id); got.Status != RequestRejected || got.RejectedBy != "alice" {
		t.Fatalf("request not marked rejected: %+v", got)
	}
}

// If the target changes between propose and approve, the CAS replay fails and
// the request is marked STALE rather than clobbering the newer content.
func TestApproveFlow_StaleBaseMarksStale(t *testing.T) {
	proposer, store, base := approveFlowFixture(t, map[string]string{"/ops/freeze": "approve@oncall"})
	// Seed an existing target so the proposal captures a base hash.
	if _, err := base.WriteFileAtomic("/ops/freeze", []byte("v1\n"), vfs.WriteOpts{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	run(proposer, "echo v2 > /ops/freeze") // pending against base v1
	id := onlyRequestID(t, store)

	// Someone else changes the target out from under the proposal.
	if _, err := base.WriteFileAtomic("/ops/freeze", []byte("v3\n"), vfs.WriteOpts{}); err != nil {
		t.Fatalf("concurrent change: %v", err)
	}

	alice := approverShell("alice", []string{"approve@oncall"})
	if _, errs, code := run(alice, "approve "+id); code == 0 || !strings.Contains(errs, "STALE") {
		t.Fatalf("stale approval should fail as STALE: code=%d err=%q", code, errs)
	}
	if b, _ := base.ReadFile("/ops/freeze"); string(b) != "v3\n" {
		t.Fatalf("stale approval must not clobber newer content, got %q", b)
	}
	if got, _ := store.Get(id); got.Status != RequestStale {
		t.Fatalf("request should be STALE, got %s", got.Status)
	}
}
