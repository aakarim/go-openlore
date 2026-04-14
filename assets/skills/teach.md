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

## Embedding Docs into a Binary

The real power of OpenLore is baking your docs into a single binary that anyone can run:

1. Clone or fork the repo:
   ```bash
   git clone https://github.com/aakarim/go-openlore
   cd go-openlore
   ```

2. Place your documentation in `assets/lore/`:
   ```bash
   rm assets/lore/PUT_YOUR_DOCS_HERE
   cp -r /path/to/your/docs/* assets/lore/
   ```

3. (Optional) Customize the config in `assets/config/openlore.yml`

4. Build the binary:
   ```bash
   go build -o my-lore ./cmd/openlore
   ```

5. Distribute the binary — it contains everything. Anyone who runs it gets an SSH server with your docs.

## Setting Up Access Control

If you want different agents to see different docs:

1. Create a `lore.json` (see `lore.json.example`):
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

2. Start with auth:
   ```bash
   openlore --auth lore.json ./docs
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

## Exporting Embedded Docs

To extract docs from an existing OpenLore binary:

```bash
openlore export -o ./extracted-docs
```
