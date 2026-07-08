package openlore

import (
	"strings"

	"github.com/aakarim/go-openlore/pkg/vfs"
)

// scopedWriteFS narrows a session's write surface to a fixed set of docset
// roots (Part B per-identity isolation). Reads pass straight through to the
// wrapped filesystem; a write (WriteFileAtomic / Mkdir) only reaches the
// backing substrate when its target sits strictly inside one of the writable
// roots, otherwise it is rejected with vfs.ErrReadOnly.
//
// This is how two agents that can both *see* the same docsets (same lore) are
// still prevented from writing each other's docsets: each session is scoped to
// the roots of the docsets it may publish to.
type scopedWriteFS struct {
	vfs.FileSystem                // read delegation (Stat / ReadDir / ReadFile)
	inner          vfs.WritableFS // the writable substrate, nil if base is read-only
	writableRoots  []string       // cleaned docset roots this session may write
}

// newScopedWriteFS wraps base so writes are confined to writableRoots. If base
// is not itself writable, every write fails closed with vfs.ErrReadOnly.
func newScopedWriteFS(base vfs.FileSystem, writableRoots []string) *scopedWriteFS {
	w, _ := base.(vfs.WritableFS)
	cleaned := make([]string, 0, len(writableRoots))
	for _, r := range writableRoots {
		cleaned = append(cleaned, vfs.CleanPath(r))
	}
	return &scopedWriteFS{FileSystem: base, inner: w, writableRoots: cleaned}
}

// inScope reports whether p sits strictly inside one of the writable roots. A
// root of "/" means the whole tree (any non-root path) is in scope.
func (s *scopedWriteFS) inScope(p string) bool {
	clean := vfs.CleanPath(p)
	for _, root := range s.writableRoots {
		if root == "/" {
			if clean != "/" {
				return true
			}
			continue
		}
		if strings.HasPrefix(clean, root+"/") {
			return true
		}
	}
	return false
}

func (s *scopedWriteFS) WriteFileAtomic(p string, data []byte, opts vfs.WriteOpts) (string, error) {
	if s.inner == nil || !s.inScope(p) {
		return "", vfs.ErrReadOnly
	}
	return s.inner.WriteFileAtomic(p, data, opts)
}

// CanWrite reports whether p is writable in this session's scope (vfs.WriteScopeFS).
// It enables fail-fast checks (e.g. `spawn`) without performing a write.
func (s *scopedWriteFS) CanWrite(p string) bool {
	return s.inner != nil && s.inScope(p)
}

func (s *scopedWriteFS) Mkdir(p string) error {
	if s.inner == nil || !s.inScope(p) {
		return vfs.ErrReadOnly
	}
	return s.inner.Mkdir(p)
}

func (s *scopedWriteFS) MkdirAll(p string) error {
	if s.inner == nil || !s.inScope(p) {
		return vfs.ErrReadOnly
	}
	return s.inner.MkdirAll(p)
}

func (s *scopedWriteFS) Remove(p string) error {
	if s.inner == nil || !s.inScope(p) {
		return vfs.ErrReadOnly
	}
	return s.inner.Remove(p)
}

func (s *scopedWriteFS) RemoveAll(p string, opts vfs.RemoveOpts) error {
	if s.inner == nil || !s.inScope(p) {
		return vfs.ErrReadOnly
	}
	return s.inner.RemoveAll(p, opts)
}

// SetWriteable / SetReadonly are no-ops: a session must not be able to toggle
// the substrate-wide write lock. The lock is owned centrally by the server
// (MergeFS.SetWriteable at startup); the session only ever narrows it.
func (s *scopedWriteFS) SetWriteable() error { return nil }
func (s *scopedWriteFS) SetReadonly() error  { return nil }

var _ vfs.WritableFS = (*scopedWriteFS)(nil)
