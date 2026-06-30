package openlore

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aakarim/go-openlore/pkg/vfs"
)

// RequestStatus is the lifecycle state of a human-gated write request (Part C).
type RequestStatus string

const (
	RequestPending  RequestStatus = "PENDING"
	RequestApproved RequestStatus = "APPROVED"
	RequestRejected RequestStatus = "REJECTED"
	// RequestStale means an approval was attempted but the target moved out
	// from under the proposal (CAS precondition failed), so the write was not
	// committed.
	RequestStale RequestStatus = "STALE"
)

// ApprovalRequest is the durable record of a write that needs a human before it
// commits. The proposed bytes and the proposal-time base are stored alongside
// the metadata so the request can be rendered (diff) and replayed (CAS) later,
// independent of what happens to the target in the meantime.
type ApprovalRequest struct {
	ID                 string        `json:"id"`
	Status             RequestStatus `json:"status"`
	TargetPath         string        `json:"target_path"`
	Action             string        `json:"action"` // "publish" | "write"
	ProposerIdentity   string        `json:"proposer_identity"`
	RequiredCapability string        `json:"required_capability"`

	// BaseExists records whether the target existed at proposal time. It picks
	// the replay precondition: IfMatch(BaseHash) when true, IfNoneMatch when
	// false.
	BaseExists   bool   `json:"base_exists"`
	BaseHash     string `json:"base_hash,omitempty"`
	ProposedHash string `json:"proposed_hash"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	ApprovedBy string `json:"approved_by,omitempty"`
	RejectedBy string `json:"rejected_by,omitempty"`
	Error      string `json:"error,omitempty"`
}

// RequestStore is a small file-backed store for approval requests. Each request
// is three files under dir: <id>.json (metadata), <id>.proposed (the proposed
// bytes), and, when the target existed at proposal time, <id>.base (the base
// bytes captured for the diff). It is safe for concurrent use.
type RequestStore struct {
	dir string
	mu  sync.Mutex
}

// NewRequestStore opens (creating if needed) a request store rooted at dir.
func NewRequestStore(dir string) (*RequestStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating request store dir: %w", err)
	}
	return &RequestStore{dir: dir}, nil
}

func newRequestID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "req_" + hex.EncodeToString(b)
}

func (s *RequestStore) metaPath(id string) string     { return filepath.Join(s.dir, id+".json") }
func (s *RequestStore) proposedPath(id string) string { return filepath.Join(s.dir, id+".proposed") }
func (s *RequestStore) basePath(id string) string     { return filepath.Join(s.dir, id+".base") }

// Create persists a new PENDING request. The ID and timestamps are assigned
// here; the caller supplies the rest of the metadata plus the proposed bytes
// and (when BaseExists) the base bytes. The metadata file is written last so a
// partially-written request never appears in List.
func (s *RequestStore) Create(req ApprovalRequest, base, proposed []byte) (ApprovalRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req.ID = newRequestID()
	now := time.Now().UTC()
	req.CreatedAt = now
	req.UpdatedAt = now
	if req.Status == "" {
		req.Status = RequestPending
	}

	if err := os.WriteFile(s.proposedPath(req.ID), proposed, 0o644); err != nil {
		return ApprovalRequest{}, fmt.Errorf("writing proposed bytes: %w", err)
	}
	if req.BaseExists {
		if err := os.WriteFile(s.basePath(req.ID), base, 0o644); err != nil {
			return ApprovalRequest{}, fmt.Errorf("writing base bytes: %w", err)
		}
	}
	if err := s.writeMeta(req); err != nil {
		return ApprovalRequest{}, err
	}
	return req, nil
}

// writeMeta atomically writes the metadata file (temp + rename).
func (s *RequestStore) writeMeta(req ApprovalRequest) error {
	data, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}
	tmp := s.metaPath(req.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing request meta: %w", err)
	}
	if err := os.Rename(tmp, s.metaPath(req.ID)); err != nil {
		return fmt.Errorf("committing request meta: %w", err)
	}
	return nil
}

// Get loads a request's metadata by id.
func (s *RequestStore) Get(id string) (ApprovalRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(id)
}

func (s *RequestStore) get(id string) (ApprovalRequest, error) {
	if !validRequestID(id) {
		return ApprovalRequest{}, os.ErrNotExist
	}
	data, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return ApprovalRequest{}, err
	}
	var req ApprovalRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return ApprovalRequest{}, fmt.Errorf("parsing request %s: %w", id, err)
	}
	return req, nil
}

// Update transitions a request, stamping UpdatedAt and rewriting its metadata.
func (s *RequestStore) Update(req ApprovalRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	req.UpdatedAt = time.Now().UTC()
	return s.writeMeta(req)
}

// Proposed returns the proposed bytes for a request.
func (s *RequestStore) Proposed(id string) ([]byte, error) {
	if !validRequestID(id) {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(s.proposedPath(id))
}

// Base returns the proposal-time base bytes (empty if the target was new).
func (s *RequestStore) Base(id string) ([]byte, error) {
	if !validRequestID(id) {
		return nil, os.ErrNotExist
	}
	data, err := os.ReadFile(s.basePath(id))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

// List returns all requests, newest first.
func (s *RequestStore) List() ([]ApprovalRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var reqs []ApprovalRequest
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		req, err := s.get(id)
		if err != nil {
			continue // skip unreadable/partial entries
		}
		reqs = append(reqs, req)
	}
	sort.Slice(reqs, func(i, j int) bool {
		return reqs[i].CreatedAt.After(reqs[j].CreatedAt)
	})
	return reqs, nil
}

// validRequestID guards against path traversal in ids that reach the FS layer.
func validRequestID(id string) bool {
	if id == "" || strings.ContainsAny(id, "/\\.") {
		return false
	}
	return true
}

// render produces the human-readable file served at /requests/<id>: metadata
// plus a base→proposed diff (or full content for a new target).
func (s *RequestStore) render(req ApprovalRequest) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "request:  %s\n", req.ID)
	fmt.Fprintf(&b, "status:   %s\n", req.Status)
	fmt.Fprintf(&b, "target:   %s\n", req.TargetPath)
	fmt.Fprintf(&b, "action:   %s\n", req.Action)
	fmt.Fprintf(&b, "proposer: %s\n", req.ProposerIdentity)
	fmt.Fprintf(&b, "requires: %s\n", req.RequiredCapability)
	if req.BaseExists {
		fmt.Fprintf(&b, "base:     existing (hash %s)\n", short(req.BaseHash))
	} else {
		fmt.Fprintf(&b, "base:     new file\n")
	}
	fmt.Fprintf(&b, "proposed: hash %s\n", short(req.ProposedHash))
	fmt.Fprintf(&b, "created:  %s\n", req.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "updated:  %s\n", req.UpdatedAt.Format(time.RFC3339))
	if req.ApprovedBy != "" {
		fmt.Fprintf(&b, "approved_by: %s\n", req.ApprovedBy)
	}
	if req.RejectedBy != "" {
		fmt.Fprintf(&b, "rejected_by: %s\n", req.RejectedBy)
	}
	if req.Error != "" {
		fmt.Fprintf(&b, "error:    %s\n", req.Error)
	}

	proposed, err := s.Proposed(req.ID)
	if err != nil {
		return nil, err
	}
	var base []byte
	if req.BaseExists {
		base, _ = s.Base(req.ID)
	}

	b.WriteString("\ndiff (base \u2192 proposed):\n")
	for _, line := range unifiedDiff(splitLines(string(base)), splitLines(string(proposed))) {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// unifiedDiff is a minimal LCS line diff producing `+`/`-`/` ` prefixed lines.
func unifiedDiff(a, b []string) []string {
	m, n := len(a), len(b)
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	var out []string
	i, j := m, n
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && a[i-1] == b[j-1]:
			out = append(out, "  "+a[i-1])
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			out = append(out, "+ "+b[j-1])
			j--
		default:
			out = append(out, "- "+a[i-1])
			i--
		}
	}
	for l, r := 0, len(out)-1; l < r; l, r = l+1, r-1 {
		out[l], out[r] = out[r], out[l]
	}
	return out
}

// RequestsFS is the read-only computed filesystem mounted at /requests. Each
// request renders as a file named by its id; the directory lists all requests.
// It is intentionally NOT a vfs.WritableFS, so any write to /requests is denied
// by MergeFS.
type RequestsFS struct {
	store *RequestStore
}

// NewRequestsFS wraps a store as a read-only computed FS.
func NewRequestsFS(store *RequestStore) *RequestsFS { return &RequestsFS{store: store} }

func (r *RequestsFS) Stat(p string) (*vfs.FileInfo, error) {
	clean := vfs.CleanPath(p)
	if clean == "/" {
		return &vfs.FileInfo{FileName: "requests", FilePath: "/", Dir: true}, nil
	}
	id := strings.TrimPrefix(clean, "/")
	req, err := r.store.Get(id)
	if err != nil {
		return nil, err
	}
	content, err := r.store.render(req)
	if err != nil {
		return nil, err
	}
	return &vfs.FileInfo{
		FileName:    id,
		FilePath:    clean,
		FileSize:    int64(len(content)),
		FileModTime: req.UpdatedAt,
	}, nil
}

func (r *RequestsFS) ReadDir(p string) ([]vfs.FileInfo, error) {
	if vfs.CleanPath(p) != "/" {
		return nil, fmt.Errorf("not a directory: %s", p)
	}
	reqs, err := r.store.List()
	if err != nil {
		return nil, err
	}
	entries := make([]vfs.FileInfo, 0, len(reqs))
	for _, req := range reqs {
		entries = append(entries, vfs.FileInfo{
			FileName:    req.ID,
			FilePath:    "/" + req.ID,
			FileModTime: req.UpdatedAt,
		})
	}
	return entries, nil
}

func (r *RequestsFS) ReadFile(p string) ([]byte, error) {
	clean := vfs.CleanPath(p)
	if clean == "/" {
		return nil, fmt.Errorf("cannot read directory")
	}
	id := strings.TrimPrefix(clean, "/")
	req, err := r.store.Get(id)
	if err != nil {
		return nil, err
	}
	return r.store.render(req)
}

// approvalBackend implements cmds.ApprovalBackend: it resolves pending requests
// by replaying the proposed write through the writable substrate's CAS path.
// commitFS is the raw substrate (the server's MergeFS) — NOT a per-session
// scoped/approval-wrapped FS — so the replay bypasses re-gating and writes
// directly under the authority of the approval itself.
type approvalBackend struct {
	store    *RequestStore
	commitFS vfs.WritableFS
}

// Approve commits req's proposed bytes via CAS using the proposal-time base as
// the precondition: IfMatch(BaseHash) when the target existed, else IfNoneMatch.
// A precondition failure marks the request STALE and is reported as an error.
func (b *approvalBackend) Approve(id, approver string, capabilities []string) (string, error) {
	req, err := b.store.Get(id)
	if err != nil {
		return fmt.Sprintf("approve: no such request: %s", id), err
	}
	if req.Status != RequestPending {
		return fmt.Sprintf("approve: %s is already %s", id, req.Status), fmt.Errorf("not pending")
	}
	if !hasCapability(capabilities, req.RequiredCapability) {
		return fmt.Sprintf("approve: denied — %s requires capability %s", id, req.RequiredCapability), fmt.Errorf("denied")
	}

	proposed, err := b.store.Proposed(id)
	if err != nil {
		return fmt.Sprintf("approve: cannot load proposed content for %s", id), err
	}

	var opts vfs.WriteOpts
	if req.BaseExists {
		h := req.BaseHash
		opts.IfMatch = &h
	} else {
		opts.IfNoneMatch = true
	}

	if _, werr := b.commitFS.WriteFileAtomic(req.TargetPath, proposed, opts); werr != nil {
		var pe *vfs.PreconditionError
		if errors.As(werr, &pe) {
			req.Status = RequestStale
			req.Error = "target changed since proposal; re-propose required"
			req.ApprovedBy = approver
			_ = b.store.Update(req)
			return fmt.Sprintf("approve: %s is STALE — %s changed since it was proposed", id, req.TargetPath), werr
		}
		return fmt.Sprintf("approve: failed to commit %s: %v", id, werr), werr
	}

	req.Status = RequestApproved
	req.ApprovedBy = approver
	if err := b.store.Update(req); err != nil {
		return "approve: write committed but failed to record approval", err
	}
	return fmt.Sprintf("Approved %s — committed %s (approved by %s)", id, req.TargetPath, approver), nil
}

// Reject marks the request rejected without writing anything.
func (b *approvalBackend) Reject(id, approver string, capabilities []string) (string, error) {
	req, err := b.store.Get(id)
	if err != nil {
		return fmt.Sprintf("reject: no such request: %s", id), err
	}
	if req.Status != RequestPending {
		return fmt.Sprintf("reject: %s is already %s", id, req.Status), fmt.Errorf("not pending")
	}
	if !hasCapability(capabilities, req.RequiredCapability) {
		return fmt.Sprintf("reject: denied — %s requires capability %s", id, req.RequiredCapability), fmt.Errorf("denied")
	}
	req.Status = RequestRejected
	req.RejectedBy = approver
	if err := b.store.Update(req); err != nil {
		return "reject: failed to record rejection", err
	}
	return fmt.Sprintf("Rejected %s (rejected by %s)", id, approver), nil
}

func hasCapability(have []string, want string) bool {
	for _, c := range have {
		if c == want {
			return true
		}
	}
	return false
}
