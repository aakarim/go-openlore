package openlore

import (
	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// GrantType decides what a named grant permits within a single docset. Core
// registers the "ro" and "rw" grants; plugins contribute others (e.g. the
// inbox plugin's "publish") via GrantTypeProvider.
//
// A grant only ever narrows. The authorizer consults it for reads and for each
// write action; its decision is then further capped by the token scope and the
// global / per-docset readonly locks (enforced by the server, not here). A
// grant name referenced by lore.json but not registered as a GrantType makes
// the server refuse to boot — fail-closed.
type GrantType interface {
	// Name is the grant identifier used in lore.json (e.g. "ro", "rw").
	Name() string
	// CanRead reports whether the grant permits reading display path p within
	// docset ds. p is a cleaned VFS display path already known to sit within the
	// docset.
	CanRead(ds config.DocsetSpec, p string) bool
	// AllowsWrite reports whether the grant ever permits writes at all. It drives
	// coarse shell action gating (whether write verbs are offered); per-op
	// authorization still runs through CanWrite.
	AllowsWrite() bool
	// CanWrite reports whether the grant permits mutation action on display path
	// p within docset ds.
	CanWrite(ds config.DocsetSpec, action vfs.ChangeAction, p string) bool
}

// GrantTypeProvider is implemented by a plugin that contributes named grant
// types. The server registers them at plugin registration so lore.json may
// reference the grant names.
type GrantTypeProvider interface {
	GrantTypes() []GrantType
}

// grantRegistry maps grant names to their behavior. Core types are registered at
// server construction; plugins add more via GrantTypeProvider.
type grantRegistry struct {
	types map[string]GrantType
}

func newGrantRegistry() *grantRegistry {
	r := &grantRegistry{types: map[string]GrantType{}}
	r.register(roGrant{})
	r.register(rwGrant{})
	return r
}

func (r *grantRegistry) register(g GrantType) { r.types[g.Name()] = g }

func (r *grantRegistry) get(name string) (GrantType, bool) {
	g, ok := r.types[name]
	return g, ok
}

// roGrant grants reading the whole docset and no writes.
type roGrant struct{}

func (roGrant) Name() string                           { return "ro" }
func (roGrant) CanRead(config.DocsetSpec, string) bool { return true }
func (roGrant) AllowsWrite() bool                      { return false }
func (roGrant) CanWrite(config.DocsetSpec, vfs.ChangeAction, string) bool {
	return false
}

// rwGrant grants reading and writing anywhere in the docset (subject to the
// per-docset readonly flag and the global write lock, enforced by the server).
type rwGrant struct{}

func (rwGrant) Name() string                           { return "rw" }
func (rwGrant) CanRead(config.DocsetSpec, string) bool { return true }
func (rwGrant) AllowsWrite() bool                      { return true }
func (rwGrant) CanWrite(config.DocsetSpec, vfs.ChangeAction, string) bool {
	return true
}
