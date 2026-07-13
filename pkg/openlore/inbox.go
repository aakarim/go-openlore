package openlore

import (
	"strings"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// InboxPlugin contributes the "publish" grant: an identity holding it may read
// the whole docset, may never delete anything, and may only create or edit files
// within the docset's configured inbox folder. It is registered like any other
// plugin (Server.RegisterPlugin) and exposes its grant via GrantTypeProvider.
//
// The inbox model lets an outside collaborator drop material into a docset (an
// "inbox") without granting them write access to the rest of the docset and
// without any ability to delete.
type InboxPlugin struct{}

// NewInboxPlugin returns the inbox plugin.
func NewInboxPlugin() *InboxPlugin { return &InboxPlugin{} }

// GrantTypes implements GrantTypeProvider.
func (*InboxPlugin) GrantTypes() []GrantType { return []GrantType{publishGrant{}} }

// Info implements PluginInfoProvider.
func (*InboxPlugin) Info() PluginInfo { return PluginInfo{Name: "inbox", Version: "1.0.0"} }

var (
	_ GrantTypeProvider  = (*InboxPlugin)(nil)
	_ PluginInfoProvider = (*InboxPlugin)(nil)
)

// publishGrant is the "publish" grant type: read anywhere in the docset, no
// deletes, create/edit only within the docset's inbox.
type publishGrant struct{}

func (publishGrant) Name() string { return "publish" }

// CanRead permits reading the whole docset.
func (publishGrant) CanRead(config.DocsetSpec, string) bool { return true }

// AllowsWrite is true: publish grants write inside the inbox.
func (publishGrant) AllowsWrite() bool { return true }

// CanWrite permits create/edit (write, mkdir) only within the docset's inbox,
// and denies every removal everywhere.
func (publishGrant) CanWrite(ds config.DocsetSpec, action vfs.ChangeAction, p string) bool {
	switch action {
	case vfs.ChangeActionRemove, vfs.ChangeActionRemoveAll:
		return false // publish can never delete
	}
	inbox := inboxPath(ds)
	if inbox == "" {
		return false // no inbox configured: publish can write nothing
	}
	clean := vfs.CleanPath(p)
	return clean == inbox || strings.HasPrefix(clean, inbox+"/")
}

// inboxPath resolves a docset's inbox to a cleaned VFS display path. An absolute
// Inbox is used verbatim; a relative Inbox is joined onto the docset's first
// display root. Returns "" when the docset has no inbox or no paths.
func inboxPath(ds config.DocsetSpec) string {
	if ds.Inbox == "" {
		return ""
	}
	if strings.HasPrefix(ds.Inbox, "/") {
		return vfs.CleanPath(ds.Inbox)
	}
	if len(ds.Paths) == 0 {
		return ""
	}
	root := ds.Paths[0].Display
	if root == "" {
		root = ds.Paths[0].Source
	}
	return vfs.CleanPath(root + "/" + ds.Inbox)
}
