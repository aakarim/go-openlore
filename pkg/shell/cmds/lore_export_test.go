package cmds

// Test-only accessor that lets the external cmds_test package remove a
// lore-subcommand registered during a test, so registrations don't leak.

// DeleteLoreSubForTest removes a registered lore subcommand by name.
func DeleteLoreSubForTest(name string) { delete(loreSubs, name) }
