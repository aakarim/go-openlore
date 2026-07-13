package openlore

// PluginInfo identifies a registered plugin: a stable name and a semantic
// version. Every built-in plugin reports one via PluginInfoProvider so the
// active plugin set (and its versions) is recorded in the server's boot logs.
type PluginInfo struct {
	// Name is the plugin's stable identifier (e.g. "okf", "shellexec").
	Name string
	// Version is the plugin's semantic version (e.g. "0.1.0").
	Version string
}

// PluginInfoProvider is implemented by a plugin that reports its identity and
// version. registerPlugin logs it at registration, so the boot logs record
// exactly which plugins — and which versions — are active.
type PluginInfoProvider interface {
	Info() PluginInfo
}
