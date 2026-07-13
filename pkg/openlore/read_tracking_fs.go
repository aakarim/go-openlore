package openlore

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/aakarim/go-openlore/pkg/vfs"
)

// readTrackingFS is the outermost per-session write-surface wrapper. It records
// the content hash of every file the session reads (and of every file it
// successfully writes) so the write seam can compare-and-swap a blind overwrite
// against the version the caller last saw — without the caller naming a hash.
//
// This is session-scoped optimistic concurrency: `cat notes.md` then later
// `echo … > notes.md` fails if notes.md changed in between, because the tracked
// last-read hash no longer matches. A successful write updates the tracked hash
// so a caller can write the same file repeatedly after a single read.
//
// It sits outside scopedWriteFS so it observes all reads and all writes, but
// inside aliasFS so aliases share canonical CAS state. It forwards the optional
// scope introspection (vfs.WriteScopeFS) used by `spawn` fail-fast checks.
type readTrackingFS struct {
	vfs.WritableFS // read/write delegation (Stat, ReadDir, SetWriteable, Mkdir, …)

	mu    sync.Mutex
	reads map[string]string // cleaned path -> last-seen content hash
}

// newReadTrackingFS wraps a writable session filesystem with read-hash tracking.
func newReadTrackingFS(inner vfs.WritableFS) *readTrackingFS {
	return &readTrackingFS{WritableFS: inner, reads: make(map[string]string)}
}

// ReadFile delegates the read and records the content hash for later CAS.
func (f *readTrackingFS) ReadFile(p string) ([]byte, error) {
	data, err := f.WritableFS.ReadFile(p)
	if err == nil {
		f.note(p, hashBytes(data))
	}
	return data, err
}

// WriteFileAtomic delegates the write and, on success, updates the tracked hash
// so repeated writes to the same file in one session chain correctly.
func (f *readTrackingFS) WriteFileAtomic(p string, data []byte, opts vfs.WriteOpts) (string, error) {
	h, err := f.WritableFS.WriteFileAtomic(p, data, opts)
	if err == nil {
		f.note(p, h)
	}
	return h, err
}

// Remove delegates the single-file/empty-dir delete and, on success, forgets
// the tracked hash for the removed path.
func (f *readTrackingFS) Remove(p string) error {
	err := f.WritableFS.Remove(p)
	if err == nil {
		f.forget(p)
	}
	return err
}

// RemoveAll delegates the whole-tree delete and, on success, forgets the
// tracked hash for the removed path and every descendant under it.
func (f *readTrackingFS) RemoveAll(p string, opts vfs.RemoveOpts) error {
	err := f.WritableFS.RemoveAll(p, opts)
	if err == nil {
		f.forgetSubtree(p)
	}
	return err
}

// LastReadHash reports the hash recorded when p was last read or written
// (vfs.ReadTracker).
func (f *readTrackingFS) LastReadHash(p string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.reads[vfs.CleanPath(p)]
	return h, ok
}

// CanWrite forwards the session's write-scope check (vfs.WriteScopeFS) so
// fail-fast callers like `spawn` still see the underlying scope through the
// wrapper.
func (f *readTrackingFS) CanWrite(p string) bool {
	if sc, ok := f.WritableFS.(vfs.WriteScopeFS); ok {
		return sc.CanWrite(p)
	}
	return true
}

func (f *readTrackingFS) note(p, hash string) {
	f.mu.Lock()
	f.reads[vfs.CleanPath(p)] = hash
	f.mu.Unlock()
}

// forget drops the tracked hash for a single path.
func (f *readTrackingFS) forget(p string) {
	f.mu.Lock()
	delete(f.reads, vfs.CleanPath(p))
	f.mu.Unlock()
}

// forgetSubtree drops the tracked hash for a path and everything under it.
func (f *readTrackingFS) forgetSubtree(p string) {
	clean := vfs.CleanPath(p)
	prefix := clean + "/"
	f.mu.Lock()
	for tracked := range f.reads {
		if tracked == clean || strings.HasPrefix(tracked, prefix) {
			delete(f.reads, tracked)
		}
	}
	f.mu.Unlock()
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

var (
	_ vfs.WritableFS   = (*readTrackingFS)(nil)
	_ vfs.ReadTracker  = (*readTrackingFS)(nil)
	_ vfs.WriteScopeFS = (*readTrackingFS)(nil)
)
