# Getting Started with OpenLore

OpenLore serves your documentation to AI agents over SSH.

## Install

```bash
go install github.com/aakarim/go-openlore/cmd/openlore@latest
```

## Serve Your Docs

```bash
openlore ./docs
```

## Connect

```bash
ssh -p 2222 localhost
```

## What Can Agents Do?

Once connected, agents can use familiar bash commands:

```bash
# List all documentation
tree -L 2 /

# Search across all docs
grep -r "authentication" /docs

# Read specific files
cat /docs/api-reference.md

# Find files by name
find / -name "*.md"

# Process JSON
cat /docs/config.json | jq '.settings'
```

## Access via MCP

OpenLore also speaks the Model Context Protocol. The main server starts an
MCP-over-HTTP endpoint **on by default** (port 8081), so MCP-aware clients like
Claude Desktop and Cowork can browse your docs without SSH:

```bash
openlore ./docs
#   MCP:  http://localhost:8081
```

See `cat /mcp.md` for stdio mode, config, and desktop-extension packaging.

## Embed Docs into a Binary

The killer feature: bake your docs into a single distributable binary.

1. Place docs in `assets/lore/`
2. Build: `go build -o my-lore ./cmd/openlore`
3. Distribute the binary — it contains everything

## Next Steps

- Read `mcp.md` to connect MCP-aware clients (Claude Desktop, Cowork, etc.)
- Run `teach` to learn how to set up OpenLore for your project
- Run `agents` to get an AGENTS.md snippet for your AI agents
- Visit https://github.com/aakarim/go-openlore for full documentation
