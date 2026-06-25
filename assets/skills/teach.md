# Setting Up OpenLore

OpenLore serves your documentation to AI agents over SSH. Here's how to set it up for your project.

## Quick Start

1. Install OpenLore:
   ```bash
   go install github.com/aakarim/go-openlore/cmd/openlore@latest
   ```

2. Serve your docs directory:
   ```bash
   openlore ./docs
   ```

3. Connect and explore:
   ```bash
   ssh -p 2222 localhost
   ```

Run this way, OpenLore serves the directory you pass on the command line and
runs keyless (any client can connect) — no config file needed.

## Embedding Docs into a Binary

The real power of OpenLore is baking your docs into a single binary that anyone
can run.

1. Clone or fork the repo:
   ```bash
   git clone https://github.com/aakarim/go-openlore
   cd go-openlore
   ```

2. Replace the bundled docs with yours. `assets/lore/` ships with OpenLore's
   own default docs (`getting-started.md`, `commands.md`, …). Remove those and
   drop your own markdown in:
   ```bash
   rm -rf assets/lore/*
   cp -r /path/to/your/docs/* assets/lore/
   ```
   Whatever lives under `assets/lore/` is what the built binary serves at `/`.

3. Replace the bundled config. **Important:** `assets/config/openlore.yml`
   ships configured for the public `openlore.sh` deployment — it sets
   `auth_file: ./lore.json`, mounts published folders, and enables `openlore.sh`
   passkeys. If you build with it unchanged, the binary fails on startup with
   `loading auth config: open ./lore.json: no such file or directory`.

   Overwrite it with a minimal config for your own build:
   ```bash
   cat > assets/config/openlore.yml <<'YAML'
   version: "1"
   port: 2222
   http_port: 8080
   default_cwd: /

   files:
     allowed: ["*.md", "*.txt", "*.yml", "*.json"]
     ignore: [".git", "node_modules", ".env"]
   YAML
   ```
   With no `auth_file` set, the server runs keyless and serves your embedded
   docs out of the box. Add `auth_file: ./lore.json` only once you actually ship
   a `lore.json` (see "Setting Up Access Control").

4. Build the binary:
   ```bash
   go build -o my-lore ./cmd/openlore
   ```

5. Run it — it contains everything:
   ```bash
   ./my-lore
   ssh -p 2222 localhost
   ```
   Distribute the binary and anyone who runs it gets an SSH server with your
   docs. No directory argument is needed: with no path on the command line, the
   binary serves its embedded `assets/lore/` docs.

## Setting Up Access Control

If you want different agents to see different docs, ship a `lore.json`. Docsets
name a set of paths; lores group docsets; identities map an SSH public key to a
lore. Start from `lore.json.example` in the repo.

```json
{
  "allow_keyless": false,
  "docsets": {
    "public":  { "paths": ["/getting-started.md", "/public"] },
    "backend": { "paths": ["/api", "/backend"] }
  },
  "lore": {
    "default": ["public"],
    "backend": ["public", "backend"]
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

- `docsets` — named groups of paths (into the served tree).
- `lore` — each lore is a **list of docset names**; an identity's lore is the
  union of those docsets' paths. `default` is the lore used by keyless/unknown
  clients.
- `identities` — bind an SSH public key to a lore.
- `allow_keyless` — `false` requires a known public key; `true` (default) lets
  any client connect as the `default` lore.

Point the server at it either by setting `auth_file: ./lore.json` in
`openlore.yml`, or with a flag:
```bash
openlore --auth lore.json ./docs
```

Add a public key without hand-editing the JSON:
```bash
openlore identity add --name my-agent --key "ssh-ed25519 AAAA..." --lore backend --auth lore.json
```

## Using the GitHub Action

For automated builds, use the OpenLore GitHub Action:

```yaml
- uses: aakarim/openlore@v1
  with:
    docs-dir: ./docs
    config: ./openlore.yml
```

This produces binaries for Linux, macOS, and Windows.

## Setting Up Passkeys (Browser Access for Humans)

Passkeys let humans browse your docs in a web browser using WebAuthn (Face ID,
Touch ID, security keys).

1. Add to your `openlore.yml`:
   ```yaml
   passkeys:
     enabled: true
     rp_id: localhost                       # or your domain
     rp_origins: ["http://localhost:8080"]  # must match the HTTP server origin
   ```

2. Start the server, connect via SSH, and register:
   ```bash
   ssh -p 2222 localhost "passkey register --name 'My Laptop'"
   ```

3. Open the printed URL in a browser to complete registration.

4. Browse docs at `http://localhost:8080/lore/`.

Run `passkey help` in the SSH shell for all options.

## Exporting Embedded Docs

To extract docs from an existing OpenLore binary:

```bash
openlore export -o ./extracted-docs
```
