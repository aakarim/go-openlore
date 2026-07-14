package openlore

import (
	"github.com/aakarim/go-openlore/pkg/openlore/meta"
	"github.com/aakarim/go-openlore/pkg/shell/cmds"
)

// CommandProvider is implemented by a plugin that contributes `lore`
// subcommands. registerPlugin detects it and installs each returned command on
// shells created by that server, so a plugin can extend the `lore`
// introspection surface without leaking state across servers. Core subcommands
// (docsets, meta) use the package registry.
type CommandProvider interface {
	LoreCommands() []cmds.LoreSub
}

// MetaExtenderProvider is implemented by a plugin that enriches `lore meta`
// records. registerPlugin detects it and collects each extender onto the server,
// which installs them per session (buildSessionShell). This is how the okf
// plugin annotates documents with OKF conformance in `lore meta` output where
// OKF applies, so read-side discovery agrees with write-side enforcement —
// without coupling the generic `lore meta` reader (pkg/meta) to the OKF spec.
type MetaExtenderProvider interface {
	MetaExtenders() []meta.Extender
}

type MetaFilterProvider interface{ MetaFilters() []meta.Filter }
