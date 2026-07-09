package cmds

// Test-only accessors that let the external cmds_test package reset the global
// lore-subcommand and meta-extender registries between tests, so registrations
// made by one test don't leak into others.

// ResetMetaExtendersForTest clears all registered meta extenders.
func ResetMetaExtendersForTest() { metaExtenders = nil }

// DeleteLoreSubForTest removes a registered lore subcommand by name.
func DeleteLoreSubForTest(name string) { delete(loreSubs, name) }
