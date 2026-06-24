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
