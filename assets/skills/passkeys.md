# Passkeys — Browser Access for Humans

Register a passkey so humans can browse docs in a web browser via WebAuthn (Face ID, Touch ID, security keys).

## Register a passkey

```bash
# Full access to all docsets
ssh openlore.sh passkey register --name "Adil MacBook" --lore agent-full

# Public docs only
ssh openlore.sh passkey register --name "Guest" --lore default
```

The command outputs a one-time URL (expires in 5 minutes). Give it to the human to open in their browser to complete registration.

## Manage passkeys

```bash
# List all registered passkeys
ssh openlore.sh passkey list

# Revoke a passkey
ssh openlore.sh passkey revoke "Guest"
```

## After registration

The human can browse docs at `https://openlore.sh/lore/`. They can also go directly to a file, e.g. `https://openlore.sh/lore/knowledge/product/data-fabric.md` — if not logged in, the passkey login flow starts automatically and redirects back to the file.

## Available lore specs

- `default` — OpenLore documentation only
- `agent-full` — all docsets (docs, knowledge, research, pipeline, runbooks, memory)
