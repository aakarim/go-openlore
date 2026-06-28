# MCP Server

OpenLore speaks the [Model Context Protocol](https://modelcontextprotocol.io)
so MCP-aware clients (Claude Desktop, Cowork, and other agents) can browse your
docs without SSH. The MCP server exposes the same filesystem as the SSH shell
through two tools:

- `shell` — run a bash command against the docs filesystem (`ls`, `cat`, `grep`,
  `find`, pipes, loops, and all the usual commands)
- `list_commands` — list every available shell command

## Always-on HTTP endpoint (default)

The main server starts an MCP-over-HTTP endpoint (Streamable HTTP transport)
**on by default**, alongside SSH and the HTTP front page:

```bash
openlore ./docs
#   SSH:  ssh -p 2222 localhost
#   HTTP: http://localhost:8080
#   MCP:  http://localhost:8081
```

Point a Streamable-HTTP MCP client at `http://localhost:8081`.

Configure it in `openlore.yml`:

```yaml
mcp:
  enabled: true   # on by default; set false to disable
  port: 8081      # set 0 to disable
```

Or with flags: `--mcp-port 9000` to change the port, `--mcp-port 0` to disable.

## Stdio (Claude Desktop, Cowork, etc.)

For clients that launch a local process and talk over stdio, run the dedicated
`mcp` subcommand:

```bash
# Serve the embedded docs over MCP (stdio)
openlore mcp

# Or serve a directory
openlore mcp ./docs

# Restrict which files are exposed
openlore mcp --allowed '*.md,*.txt' --ignore '.git,node_modules' ./docs
```

Point your MCP client at the command. For example, in a `mcpServers` config:

```json
{
  "mcpServers": {
    "openlore": {
      "command": "openlore",
      "args": ["mcp", "./docs"]
    }
  }
}
```

## Package as a desktop extension (.mcpb)

For one-click installation in Claude Desktop, package the binary (with its
embedded docs) as an MCPB extension:

```bash
go build -o openlore ./cmd/openlore
./openlore mcpb -o openlore.mcpb
```

Double-click the resulting `.mcpb` file (or drag it into Claude Desktop) to
install. If the binary has no embedded docs, the user is prompted to select a
docs directory on install. Add `--docs-dir ./docs` to bundle a directory.
