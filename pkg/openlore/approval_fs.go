package openlore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"strings"

	"github.com/aakarim/go-openlore/pkg/openlore/eventbus"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// approvalFS is the per-session write wrapper that turns writes to gated paths
// (Part C) into pending requests instead of committing them. Reads pass through
// unchanged. It sits *inside* scopedWriteFS, so an out-of-scope write is denied
// before it can ever become an approval request.
//
//	scopedWriteFS (scope gate) → approvalFS (approval gate) → base substrate
//
// A write to a gated path: the original precondition (IfMatch/IfNoneMatch) is
// honored first (a stale write fails immediately, no request is created), then
// the proposal-time base is captured and a PENDING request is recorded, and the
// call returns vfs.PendingApprovalError. A write to a non-gated path commits
// normally through the inner FS.
type approvalFS struct {
	vfs.FileSystem                // read delegation
	inner          vfs.WritableFS // the writable substrate (nil if read-only)
	store          *RequestStore
	decide         func(path string) (capability string, needsApproval bool)
	proposer       string        // identity name recording the request
	bus            *eventbus.Bus // optional; nil means no approval_pending fanout
}

func newApprovalFS(base vfs.FileSystem, store *RequestStore, decide func(string) (string, bool), proposer string, bus *eventbus.Bus) *approvalFS {
	w, _ := base.(vfs.WritableFS)
	return &approvalFS{FileSystem: base, inner: w, store: store, decide: decide, proposer: proposer, bus: bus}
}

func (a *approvalFS) WriteFileAtomic(p string, data []byte, opts vfs.WriteOpts) (string, error) {
	if a.inner == nil {
		return "", vfs.ErrReadOnly
	}
	capability, needs := a.decide(p)
	if !needs {
		return a.inner.WriteFileAtomic(p, data, opts)
	}

	clean := vfs.CleanPath(p)

	// Capture the target's current state once; it is both the precondition
	// check input and the proposal-time base.
	cur, exists := a.currentBytes(clean)
	curHash := ""
	if exists {
		curHash = hashHex(cur)
	}

	// Honor the caller's original precondition before recording a request, so a
	// stale or conflicting write fails immediately rather than parking a doomed
	// request for a human.
	if opts.IfNoneMatch && exists {
		return "", &vfs.PreconditionError{Path: clean, Current: curHash}
	}
	if opts.IfMatch != nil && (!exists || curHash != *opts.IfMatch) {
		return "", &vfs.PreconditionError{Path: clean, Current: curHash}
	}

	req := ApprovalRequest{
		TargetPath:         clean,
		Action:             "write",
		ProposerIdentity:   a.proposer,
		RequiredCapability: capability,
		BaseExists:         exists,
		ProposedHash:       hashHex(data),
	}
	var baseBytes []byte
	if exists {
		baseBytes = cur
		req.BaseHash = curHash
	}
	saved, err := a.store.Create(req, baseBytes, data)
	if err != nil {
		return "", err
	}
	if a.bus != nil {
		_ = a.bus.Publish(context.Background(), eventbus.Event{
			Kind:        eventbus.KindApprovalPending,
			Path:        clean,
			Agent:       a.proposer,
			ContentHash: saved.ProposedHash,
			Bytes:       len(data),
			Extra: map[string]string{
				"request_id": saved.ID,
				"capability": capability,
			},
		})
	}
	return "", &vfs.PendingApprovalError{RequestID: saved.ID, Capability: capability, Target: clean}
}

// currentBytes returns the bytes currently at p and whether it exists.
func (a *approvalFS) currentBytes(p string) ([]byte, bool) {
	b, err := a.FileSystem.ReadFile(p)
	if err != nil {
		return nil, false
	}
	return b, true
}

// Mkdir is not gated — directory creation carries no content to review.
func (a *approvalFS) Mkdir(p string) error {
	if a.inner == nil {
		return vfs.ErrReadOnly
	}
	return a.inner.Mkdir(p)
}

func (a *approvalFS) SetWriteable() error {
	if a.inner == nil {
		return vfs.ErrReadOnly
	}
	return a.inner.SetWriteable()
}

func (a *approvalFS) SetReadonly() error {
	if a.inner == nil {
		return vfs.ErrReadOnly
	}
	return a.inner.SetReadonly()
}

var _ vfs.WritableFS = (*approvalFS)(nil)

// hashHex is the hex SHA-256 of b — the same content hash CAS uses.
func hashHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// requiresApproval reports whether a write to virtual path p is gated, and by
// which capability. It scans every docset's RequiresApproval rules.
func (s *Server) requiresApproval(p string) (string, bool) {
	if s.auth == nil {
		return "", false
	}
	clean := vfs.CleanPath(p)
	for _, ds := range s.auth.Docsets {
		for _, rule := range ds.RequiresApproval {
			for _, pat := range rule.Paths {
				if approvalPathMatch(pat, clean) {
					return rule.Capability, true
				}
			}
		}
	}
	return "", false
}

// hasApprovalRules reports whether any docset declares an approval rule.
func (s *Server) hasApprovalRules() bool {
	if s.auth == nil {
		return false
	}
	for _, ds := range s.auth.Docsets {
		if len(ds.RequiresApproval) > 0 {
			return true
		}
	}
	return false
}

// approvalPathMatch matches a virtual path against a rule pattern. A trailing
// "/**" matches the subtree (and the prefix itself); a pattern containing "*"
// uses path.Match per segment; otherwise the match is exact.
func approvalPathMatch(pattern, p string) bool {
	pattern = vfs.CleanPath(pattern)
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return p == prefix || strings.HasPrefix(p, prefix+"/")
	}
	if strings.Contains(pattern, "*") {
		ok, err := path.Match(pattern, p)
		return err == nil && ok
	}
	return pattern == p
}
