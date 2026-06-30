package openlore

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/openlore/eventbus"
	"github.com/aakarim/go-openlore/pkg/shell"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
)

// recordingSub captures every event it receives for assertions.
type recordingSub struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (r *recordingSub) Name() string   { return "test-recorder" }
func (r *recordingSub) Required() bool { return false }
func (r *recordingSub) Handle(_ context.Context, e eventbus.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recordingSub) kinds() []eventbus.EventKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]eventbus.EventKind, len(r.events))
	for i, e := range r.events {
		out[i] = e.Kind
	}
	return out
}

func (r *recordingSub) firstOf(k eventbus.EventKind) (eventbus.Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.Kind == k {
			return e, true
		}
	}
	return eventbus.Event{}, false
}

// TestApprovalEvents_PendingThenPostWrite asserts the two Slice 5 notify
// moments fire on the bus: approval_pending when a gated write is parked as a
// request, and post_write when the approver later commits it.
func TestApprovalEvents_PendingThenPostWrite(t *testing.T) {
	rec := &recordingSub{}
	bus := eventbus.New(nil)
	bus.Subscribe(rec)

	base := NewDirFS(t.TempDir(), config.FilesConfig{}).WithBus(bus)
	if err := base.SetWriteable(); err != nil {
		t.Fatalf("SetWriteable: %v", err)
	}
	if err := base.Mkdir("/ops"); err != nil {
		t.Fatalf("seed mkdir: %v", err)
	}

	store, err := NewRequestStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewRequestStore: %v", err)
	}
	saved := cmds.Approvals
	cmds.Approvals = &approvalBackend{store: store, commitFS: base}
	t.Cleanup(func() { cmds.Approvals = saved })

	decide := func(p string) (string, bool) {
		if p == "/ops/freeze" {
			return "approve@oncall", true
		}
		return "", false
	}
	af := newApprovalFS(base, store, decide, "claude", bus)
	scoped := newScopedWriteFS(af, []string{"/ops"})
	proposer := shell.NewShell(scoped)
	proposer.SetEnv("OPENLORE_DOCSETS", "ops")

	// Propose: gated write parks a request and emits approval_pending only.
	if _, errs, code := run(proposer, "echo ON > /ops/freeze"); code != 0 || !strings.Contains(errs, "pending approval") {
		t.Fatalf("propose: code=%d err=%q", code, errs)
	}
	pending, ok := rec.firstOf(eventbus.KindApprovalPending)
	if !ok {
		t.Fatalf("no approval_pending event, got kinds %v", rec.kinds())
	}
	if pending.Path != "/ops/freeze" || pending.Agent != "claude" {
		t.Fatalf("approval_pending payload = %+v", pending)
	}
	if pending.Extra["request_id"] == "" || pending.Extra["capability"] != "approve@oncall" {
		t.Fatalf("approval_pending extra = %v", pending.Extra)
	}
	if _, ok := rec.firstOf(eventbus.KindPostWrite); ok {
		t.Fatal("post_write must not fire while pending")
	}

	id := pending.Extra["request_id"]

	// Approve: the committed write emits post_write.
	alice := approverShell("alice", []string{"approve@oncall"})
	if out, errs, code := run(alice, "approve "+id); code != 0 {
		t.Fatalf("approve failed: code=%d out=%q err=%q", code, out, errs)
	}
	pw, ok := rec.firstOf(eventbus.KindPostWrite)
	if !ok {
		t.Fatalf("no post_write event after approve, got kinds %v", rec.kinds())
	}
	if pw.Path != "/ops/freeze" {
		t.Fatalf("post_write payload = %+v", pw)
	}
}
