# Authentication & Access Control

## Keyless (Default)

By default, any SSH client can connect. No keys required.

## Public Key Auth

Create a `lore.json` to control per-agent access:

```json
{
  "lore": {
    "default": { "paths": ["/docs/public"] },
    "backend": { "paths": ["/docs/api", "/docs/backend"] }
  },
  "identities": [
    {
      "name": "my-agent",
      "public_key": "ssh-ed25519 AAAA...",
      "lore": "backend"
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

In `openlore.yml`:
- `unknown_identity: allow` — unrecognized keys get the "default" lore spec
- `unknown_identity: deny` — unrecognized keys are rejected
