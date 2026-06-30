# Authentication & Access Control

## Keyless (Default)

By default, any SSH client can connect. No keys required.

## Docsets & Lore

Access control is built on two concepts:

- **Docsets** — atomic document collections with path mappings (an agent's workspace)
- **Lore** — named compositions of docsets (what an identity can access)

Each identity references a lore name, which resolves to one or more docsets. When multiple docsets are in a lore, each appears as a subdirectory.

## Publishing

Docsets can be made writable by adding `publish_dir` to the docset config in `lore.json`:

```json
"backend": { "paths": ["/docs/api", "/docs/backend"], "publish_dir": "./published/backend" }
```

The `publish_dir` is a directory on disk where published files are written. If it falls within the served directory tree, published files appear in the VFS immediately.

Use the `publish` command to write files:

```bash
echo "content" | publish <docset> <path>
```

Running `publish` with no args lists writable docsets.

Remote usage over SSH:

```bash
echo "content" | ssh -p 2222 server publish <docset> <path>
```

## Write Conflict Policy

When the writable substrate is enabled (`readonly: false`), whole-file overwrite
verbs — `>`, `tee`, `sed -i`, and `publish` — follow a configurable conflict
policy:

- **`hash`** (default) — overwrites are **compare-and-swap**. The write carries
  the hash of the content it is based on; if the file changed since then, the
  write is rejected with `file changed concurrently — re-read and retry` instead
  of silently clobbering the other change. For read-modify-write verbs (`sed -i`)
  the base is exactly what was transformed, giving true optimistic concurrency;
  for a blind redirect it is the content read at command time.
- **`last_write_wins`** — overwrites are unconditional (atomic, but the last
  writer silently wins).

Append (`>>`) and `patch` are **always** compare-and-swap, regardless of policy.

Set the global default in `openlore.yml`:

```yaml
write_conflict_policy: hash   # or last_write_wins
```

Override it per docset in `lore.json` (takes precedence for paths in that docset):

```json
"scratch": {
  "paths": ["/scratch"],
  "publish_dir": "./published/scratch",
  "write_conflict_policy": "last_write_wins"
}
```

## Public Key Auth

Create a `lore.json` to control per-agent access:

```json
{
  "docsets": {
    "public": { "paths": ["/docs/public"] },
    "backend": { "paths": ["/docs/api", "/docs/backend"], "publish_dir": "./published/backend" },
    "frontend": { "paths": ["/docs/frontend", "/docs/shared"] }
  },
  "lore": {
    "default": ["public"],
    "backend": ["public", "backend"],
    "engineering": ["public", "backend", "frontend"]
  },
  "identities": [
    {
      "name": "backend-agent",
      "public_key": "ssh-ed25519 AAAA...",
      "lore": "backend"
    },
    {
      "name": "eng-lead",
      "public_key": "ssh-ed25519 AAAA...",
      "lore": "engineering"
    }
  ]
}
```

## Adding Identities

Use the CLI:

```bash
openlore identity add \
  --name my-agent \
  --key "ssh-ed25519 AAAA..." \
  --lore backend \
  --auth ./lore.json
```

## Unknown Identity Handling

In `lore.json`:
- `"unknown_identity": "allow"` — unrecognized keys get the "default" lore
- `"unknown_identity": "deny"` — unrecognized keys are rejected
