package openlore

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// validateGrants verifies that every grant name referenced by the auth config
// (the anonymous default and each identity's docset grants) is a registered
// grant type. It runs at startup, after plugins have registered their grant
// types, so a config referencing an unregistered grant (e.g. `publish` without
// the inbox plugin) fails closed rather than silently denying all access.
func (s *Server) validateGrants() error {
	if !s.authEnforced {
		return nil
	}
	check := func(where, grant string) error {
		if _, ok := s.grants.get(grant); !ok {
			return fmt.Errorf("auth config: %s references unregistered grant %q (no plugin provides it)", where, grant)
		}
		return nil
	}
	for docset, grant := range s.auth.Default {
		if err := check(fmt.Sprintf("default grant for docset %q", docset), grant); err != nil {
			return err
		}
	}
	for _, ident := range s.auth.Identities {
		for docset, grant := range ident.Docsets {
			if err := check(fmt.Sprintf("identity %q grant for docset %q", ident.Name, docset), grant); err != nil {
				return err
			}
		}
	}
	return nil
}

// displayPath returns a path mapping's cleaned display (virtual) path, falling
// back to its source when no display override is set.
func displayPath(pm config.PathMapping) string {
	d := pm.Display
	if d == "" {
		d = pm.Source
	}
	return vfs.CleanPath(d)
}

// grantForPath resolves the most-specific docset the identity holds a grant on
// that contains display path p, returning the docset spec and its grant type. A
// path may sit inside several granted docsets (nesting); the longest matching
// display root wins so a nested docset overrides its parent.
func (s *Server) grantForPath(id Identity, p string) (config.DocsetSpec, GrantType, bool) {
	clean := vfs.CleanPath(p)
	bestLen := -1
	var bestDS config.DocsetSpec
	var bestGrant GrantType
	for name, grantName := range id.Grants {
		ds, ok := s.auth.Docsets[name]
		if !ok {
			continue
		}
		grant, ok := s.grants.get(grantName)
		if !ok {
			continue
		}
		for _, pm := range ds.Paths {
			root := displayPath(pm)
			if pathWithinRoot(root, clean) && len(root) > bestLen {
				bestLen = len(root)
				bestDS = ds
				bestGrant = grant
			}
		}
	}
	if bestLen < 0 {
		return config.DocsetSpec{}, nil, false
	}
	return bestDS, bestGrant, true
}

// identityCanWrite is the per-operation write authorizer. A mutation to display
// path p is permitted only when the global lock is open, the token scope grants
// write, the identity holds a grant on the docset containing p, that docset is
// not per-docset readonly, and the grant permits the action on that path.
func (s *Server) identityCanWrite(id Identity, action vfs.ChangeAction, p string) bool {
	if s.config.Readonly {
		return false // global write lock closed
	}
	if !scopeGrantsWrite(id.Scopes) {
		return false // token scope ceiling: only full authority may write
	}
	ds, grant, ok := s.grantForPath(id, p)
	if !ok {
		return false
	}
	if ds.Readonly != nil && *ds.Readonly {
		return false // per-docset lock can only further restrict
	}
	return grant.CanWrite(ds, action, p)
}

// readableRoots returns the display roots the identity may read: the display
// paths of every docset it holds a readable grant on, plus every system mount
// (control-plane mounts like /jobs and /requests are always visible). Used to
// build the session's read-scoping filesystem.
func (s *Server) readableRoots(id Identity) []string {
	var roots []string
	for name, grantName := range id.Grants {
		ds, ok := s.auth.Docsets[name]
		if !ok {
			continue
		}
		grant, ok := s.grants.get(grantName)
		if !ok {
			continue
		}
		for _, pm := range ds.Paths {
			root := displayPath(pm)
			if grant.CanRead(ds, root) {
				roots = append(roots, root)
			}
		}
	}
	roots = append(roots, s.merge.SystemMountPaths()...)
	return roots
}

// scopedReadFS confines a session's reads (Stat / ReadDir / ReadFile) to a fixed
// set of display roots, plus the ancestor directories that lead down to them so
// the tree stays navigable. A ReadDir at an ancestor lists only entries that are
// themselves readable (a granted root, or an ancestor of one), so an identity
// only ever sees the docsets it was granted — never the sibling docsets sharing
// the same backing root filesystem.
type scopedReadFS struct {
	vfs.FileSystem
	roots []string // cleaned readable display roots
}

func newScopedReadFS(base vfs.FileSystem, roots []string) *scopedReadFS {
	cleaned := make([]string, 0, len(roots))
	for _, r := range roots {
		cleaned = append(cleaned, vfs.CleanPath(r))
	}
	return &scopedReadFS{FileSystem: base, roots: cleaned}
}

// within reports whether p sits at or under one of the readable roots.
func (s *scopedReadFS) within(p string) bool {
	clean := vfs.CleanPath(p)
	for _, root := range s.roots {
		if pathWithinRoot(root, clean) {
			return true
		}
	}
	return false
}

// ancestor reports whether p is a proper ancestor directory of some readable
// root (so it may be listed/stat'd to navigate down toward that root).
func (s *scopedReadFS) ancestor(p string) bool {
	clean := vfs.CleanPath(p)
	if clean == "/" {
		return true
	}
	for _, root := range s.roots {
		if strings.HasPrefix(root, clean+"/") {
			return true
		}
	}
	return false
}

// readable reports whether p may be stat'd or listed: it is within a root or is
// an ancestor of one.
func (s *scopedReadFS) readable(p string) bool {
	return s.within(p) || s.ancestor(p)
}

func (s *scopedReadFS) Stat(p string) (*vfs.FileInfo, error) {
	if !s.readable(p) {
		return nil, fs.ErrNotExist
	}
	return s.FileSystem.Stat(p)
}

func (s *scopedReadFS) ReadFile(p string) ([]byte, error) {
	if !s.within(p) {
		return nil, fs.ErrNotExist
	}
	return s.FileSystem.ReadFile(p)
}

func (s *scopedReadFS) ReadDir(p string) ([]vfs.FileInfo, error) {
	if !s.readable(p) {
		return nil, fs.ErrNotExist
	}
	entries, err := s.FileSystem.ReadDir(p)
	if err != nil {
		return nil, err
	}
	clean := vfs.CleanPath(p)
	// Fully inside a readable root: every entry is readable.
	if s.within(clean) {
		return entries, nil
	}
	// At an ancestor: keep only children that are themselves readable.
	out := entries[:0]
	for _, e := range entries {
		child := clean
		if child == "/" {
			child = "/" + e.FileName
		} else {
			child = clean + "/" + e.FileName
		}
		if s.readable(child) {
			out = append(out, e)
		}
	}
	return out, nil
}
