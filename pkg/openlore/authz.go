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
	// Two docsets sharing an identical display root are an ambiguous access
	// boundary: read scoping resolves the governing docset by root length while
	// write authorization (mostSpecificDocset) breaks ties by name, so the two
	// could disagree on who governs the shared subtree. Fail closed at startup
	// rather than serve an inconsistent authorization model.
	owner := map[string]string{}
	for name, ds := range s.auth.Docsets {
		for _, pm := range ds.Paths {
			root := displayPath(pm)
			if other, dup := owner[root]; dup {
				return fmt.Errorf("auth config: docsets %q and %q share display root %q (ambiguous access boundary)", other, name, root)
			}
			owner[root] = name
		}
	}
	// Aliases are rewritten before authorization. Reject ambiguous namespace
	// shapes so no alias can shadow or bypass another docset boundary.
	type rootSpec struct {
		docset string
		path   string
		alias  bool
	}
	var roots []rootSpec
	for name, ds := range s.auth.Docsets {
		for _, pm := range ds.Paths {
			roots = append(roots, rootSpec{docset: name, path: displayPath(pm)})
		}
		if len(ds.Aliases) > 0 && len(ds.Paths) == 0 {
			return fmt.Errorf("auth config: docset %q declares aliases without a canonical path", name)
		}
		for _, raw := range ds.Aliases {
			if !strings.HasPrefix(raw, "/") {
				return fmt.Errorf("auth config: docset %q alias %q must be absolute", name, raw)
			}
			clean := vfs.CleanPath(raw)
			if clean != raw {
				return fmt.Errorf("auth config: docset %q alias %q must be normalized as %q", name, raw, clean)
			}
			roots = append(roots, rootSpec{docset: name, path: clean, alias: true})
		}
	}
	if s.merge != nil {
		for _, mount := range s.merge.mountPaths() {
			roots = append(roots, rootSpec{docset: "<mount>", path: mount})
		}
	}
	for i, candidate := range roots {
		if !candidate.alias {
			continue
		}
		for j, other := range roots {
			if i == j {
				continue
			}
			if pathWithinRoot(candidate.path, other.path) || pathWithinRoot(other.path, candidate.path) {
				kind := "canonical path"
				if other.alias {
					kind = "alias"
				}
				return fmt.Errorf("auth config: docset %q alias %q overlaps docset %q %s %q", candidate.docset, candidate.path, other.docset, kind, other.path)
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

func primaryDisplayPath(ds config.DocsetSpec) string {
	if len(ds.Paths) == 0 {
		return ""
	}
	return displayPath(ds.Paths[0])
}

// canonicalPath resolves aliases across all configured docsets. Validation
// rejects overlaps, but longest-prefix selection keeps this helper deterministic
// before startup validation and for direct library use.
func (s *Server) canonicalPath(p string) string {
	clean := vfs.CleanPath(p)
	bestAlias := ""
	bestTarget := ""
	for _, ds := range s.auth.Docsets {
		target := primaryDisplayPath(ds)
		for _, raw := range ds.Aliases {
			alias := vfs.CleanPath(raw)
			if pathWithinRoot(alias, clean) && len(alias) > len(bestAlias) {
				bestAlias = alias
				bestTarget = target
			}
		}
	}
	if bestAlias == "" {
		return clean
	}
	return replacePathRoot(clean, bestAlias, bestTarget)
}

func (s *Server) aliasesForIdentity(id Identity) []pathAlias {
	var aliases []pathAlias
	for name, grantName := range id.Grants {
		ds, ok := s.auth.Docsets[name]
		if !ok {
			continue
		}
		grant, ok := s.grants.get(grantName)
		if !ok {
			continue
		}
		target := primaryDisplayPath(ds)
		if target == "" || !grant.CanRead(ds, target) {
			continue
		}
		for _, alias := range ds.Aliases {
			aliases = append(aliases, pathAlias{Alias: alias, Target: target})
		}
	}
	return aliases
}

// mostSpecificDocset resolves the single docset that governs display path p: the
// one whose display root is the longest prefix of p, across ALL configured
// docsets (not just the ones an identity holds a grant on). Every docset is an
// access boundary that carves its subtree out of any ancestor docset, so this is
// the authority that decides access to p — a grant on an ancestor docset never
// reaches into a nested docset. Ties on root length are broken by docset name
// for determinism (overlapping identical roots are an ambiguous config).
func (s *Server) mostSpecificDocset(p string) (string, config.DocsetSpec, bool) {
	clean := s.canonicalPath(p)
	bestLen := -1
	bestName := ""
	var bestDS config.DocsetSpec
	for name, ds := range s.auth.Docsets {
		for _, pm := range ds.Paths {
			root := displayPath(pm)
			if !pathWithinRoot(root, clean) {
				continue
			}
			if len(root) > bestLen || (len(root) == bestLen && name < bestName) {
				bestLen = len(root)
				bestName = name
				bestDS = ds
			}
		}
	}
	if bestLen < 0 {
		return "", config.DocsetSpec{}, false
	}
	return bestName, bestDS, true
}

// grantForPath resolves the grant that governs display path p for this identity.
// It finds the most-specific docset covering p (across all docsets) and returns
// its grant only if the identity actually holds one on THAT docset. A grant on
// an ancestor docset (e.g. the root docset) does not authorize p when a nested
// docset carves it out and the identity has no grant on the nested docset.
func (s *Server) grantForPath(id Identity, p string) (config.DocsetSpec, GrantType, bool) {
	name, ds, ok := s.mostSpecificDocset(p)
	if !ok {
		return config.DocsetSpec{}, nil, false
	}
	grantName, ok := id.Grants[name]
	if !ok {
		return config.DocsetSpec{}, nil, false // carved out: no grant on the governing docset
	}
	grant, ok := s.grants.get(grantName)
	if !ok {
		return config.DocsetSpec{}, nil, false
	}
	return ds, grant, true
}

// allDocsetRoots returns the display roots of every configured docset — the full
// set of access boundaries used to carve nested docsets out of ancestor grants
// during read scoping.
func (s *Server) allDocsetRoots() []string {
	var roots []string
	for _, ds := range s.auth.Docsets {
		for _, pm := range ds.Paths {
			roots = append(roots, displayPath(pm))
		}
	}
	return roots
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
	p = s.canonicalPath(p)
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

// scopedReadFS confines a session's reads (Stat / ReadDir / ReadFile) to the
// display roots it may read, plus the ancestor directories that lead down to them
// so the tree stays navigable. Every configured docset root is also an access
// boundary: a path is only readable when the MOST-SPECIFIC docset covering it is
// one of the readable roots. This means a read grant on an ancestor docset (e.g.
// the root docset "/") does not reach into a nested docset the identity has no
// grant on — the nested docset overrides the ancestor. So an identity only ever
// sees the docsets it was granted, never the sibling (or carved-out nested)
// docsets sharing the same backing filesystem.
type scopedReadFS struct {
	vfs.FileSystem
	roots      []string // cleaned readable display roots (granted docsets + system mounts)
	boundaries []string // cleaned display roots of ALL docsets (carve-out boundaries)
}

func newScopedReadFS(base vfs.FileSystem, roots, boundaries []string) *scopedReadFS {
	clean := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, r := range in {
			out = append(out, vfs.CleanPath(r))
		}
		return out
	}
	return &scopedReadFS{FileSystem: base, roots: clean(roots), boundaries: clean(boundaries)}
}

// within reports whether p is readable: some readable root covers it, and no
// more-specific docset boundary carves it out. The governing authority for p is
// the longest docset root that is a prefix of p; p is readable only when that
// governing root is itself a readable root (or p sits under a readable
// non-docset root like a system mount, with no deeper docset boundary).
func (s *scopedReadFS) within(p string) bool {
	clean := vfs.CleanPath(p)
	bestRoot := ""
	for _, root := range s.roots {
		if pathWithinRoot(root, clean) && (bestRoot == "" || len(root) > len(bestRoot)) {
			bestRoot = root
		}
	}
	if bestRoot == "" {
		return false // no readable root covers p
	}
	bestBoundary := ""
	for _, b := range s.boundaries {
		if pathWithinRoot(b, clean) && (bestBoundary == "" || len(b) > len(bestBoundary)) {
			bestBoundary = b
		}
	}
	// A docset boundary strictly more specific than the best readable root means
	// a nested docset the identity lacks a grant on carves p out.
	if len(bestBoundary) > len(bestRoot) {
		return false
	}
	return true
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
	// Keep only children that are themselves readable. This filters at every
	// level (not just ancestors): a directory fully inside a readable root can
	// still contain a nested docset the identity lacks a grant on, which must be
	// carved out of the listing.
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
