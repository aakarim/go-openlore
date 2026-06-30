# 📜 OpenLore

**Serve your docs to AI agents over SSH.**

---

## The Problem

AI coding agents — Claude, GPT, Cursor, Codex — are trained on bash. They explore codebases with `ls`, `cat`, `grep`, and `find`. It's their native interface.

But when they need your documentation, they're stuck with fragile MCP servers, RAG pipelines, or copy-pasting into context windows. These approaches are complex to set up, hard to debug, and add layers of abstraction between the agent and the content.

Also, you may need to explore those docs, to see what the agent is up to, and you don't want your chat UI or terminal to be cluttered with long raw markdown files. Sometimes you don't want markdown, sometimes it might be better to view things as a dynamic html file. Hey, you might even want to be able to run doom in the browser. 

## The Solution

The solution is filesystems everywhere.

OpenLore gives agents the same interface they already know — **a bash shell over SSH** — but serving your docs instead of a real filesystem.

It's a single binary, zero-config, read-only SSH server backed by an in-memory bash interpreter. No real processes. No shell injection. No escapes.

```
Agent → SSH → OpenLore → Your Docs
```

## Use Cases
- **Documentation access** — Serve your docs over SSH. Agents can `ssh -p 2222 docs.internal` and use `ls`, `cat`, `grep`, `find`, and more to explore.
- **A Remote View Layer for Agent Artifacts** — Agents can upload files to their workspace, and OpenLore can serve those files back over SSH. This gives agents a secure way to share artifacts, logs, screenshots, and more with the user, without needing to build a custom file upload UI or use something like Tailscale. You can access the agent's workspace remotely from anywhere using Passkeys through the browser, and review docs, logs, and screenshots that the agent wants to share with you.
- **A Remote View Layer for the Agent's Workspace** - Provide agents with a secure way to upload files, have it accessible remotely from anywhere, without having to use something like Tailscale. This means you can access your files securely using Passkeys through the browser and review docs remotely.
- **Manage Multi-Agent Knowledge Sharing** — Each agent can have its own OpenLore server with different docs. Agents can share knowledge by connecting to each other's servers, pushing and pulling context notes, and building a shared knowledge base. Or all the agents can share a single server with different directories for each agent.

## Quick Start

The fastest way to get going is to let your agent set everything up. Pipe the
`teach` skill straight into your agent CLI:

```bash
# Teach your agent how to set up OpenLore
ssh openlore.sh teach | your-agent-cli

# Add documentation access instructions to AGENTS.md
ssh openlore.sh agents >> AGENTS.md
```

The `teach` skill walks your agent through installing OpenLore, embedding docs,
building a distributable binary, and optionally setting up per-agent access
control.

### Manual setup

If you'd rather drive it yourself:

```bash
# Install
go install github.com/aakarim/go-openlore/cmd/openlore@latest

# Serve a directory
openlore ./docs

# Connect from any terminal
ssh -p 2222 localhost

# Or run commands directly
ssh -p 2222 localhost "grep -r 'authentication' /docs"
ssh -p 2222 localhost "find / -name '*.md' | head -20"
ssh -p 2222 localhost "cat /docs/api-reference.md"
```

## Embedding Docs into a Binary

The key feature of OpenLore is baking your docs into a single binary using Go's `embed` package:

1. Place your docs in `assets/lore/`
2. Build: `go build -o my-docs ./cmd/openlore`
3. Distribute the binary — it contains everything

Anyone who runs the binary gets an SSH server with your docs. This is how knowledge gets distributed — agents can spin up their own documentation servers and share lore with other agents.

### Using the GitHub Action

Automate binary builds with the OpenLore GitHub Action:

```yaml
- uses: aakarim/openlore@v1
  with:
    docs-dir: ./docs
    config: ./openlore.yml
```

This produces cross-platform binaries (Linux, macOS, Windows) with your docs embedded.

### Exporting Embedded Docs

Extract docs from an existing binary:

```bash
openlore export -o ./extracted-docs
```

## How It Works

OpenLore is built on [Wish](https://github.com/charmbracelet/wish) from Charmbracelet for the SSH transport layer. When a client connects:

1. **SSH handshake** — standard SSH protocol, key exchange, optional public key auth
2. **Shell session** — the client gets a bash-like prompt backed by an in-memory interpreter
3. **Command execution** — commands like `ls`, `cat`, `grep` are implemented as pure Go functions operating on a read-only virtual filesystem
4. **SFTP subsystem** — clients can also mount docs via `sshfs` for IDE integration

### Supported Commands

**Filesystem**

| Command | Description |
|---------|-------------|
| `ls` | List directory contents (`-l`, `-a`, `-R`, `-S`, `-t`, `-F`, `-1`, `-h`, `-d`) |
| `cat` | Display file contents (`-n`, `-A`) |
| `head` | First N lines or bytes (`-n N`, `-c N`) |
| `tail` | Last N lines or bytes (`-n N`, `-c N`, `+N`) |
| `tree` | Directory tree visualization (`-L depth`, `-a`, `-d`, `-f`) |
| `stat` | File metadata |
| `wc` | Count lines, words, bytes (`-l`, `-w`, `-c`, `-m`) |
| `du` | Estimate file space usage (`-a`, `-h`, `-s`, `-c`) |
| `diff` | Compare two files (`-u`, `-q`) |
| `cd` / `pwd` | Navigate the virtual filesystem |

**Search**

| Command | Description |
|---------|-------------|
| `grep` | Search for patterns (`-i`, `-n`, `-r`, `-v`, `-c`, `-l`, `-o`, `-L`, `-w`, `-x`, `-m`) |
| `find` | Find files (`-name`, `-type f\|d`) |

**Text Processing**

| Command | Description |
|---------|-------------|
| `sort` | Sort lines (`-r`, `-n`, `-u`, `-f`, `-k N`, `-t SEP`) |
| `uniq` | Filter duplicate lines (`-c`, `-d`, `-i`, `-u`) |
| `cut` | Cut sections from lines (`-d DEL`, `-f FIELDS`, `-c CHARS`, `-s`) |
| `sed` | Stream editor (`-n`, `-e`, `s/pat/repl/flags`) |
| `awk` | Pattern scanning and processing (`-F SEP`, `-v VAR=VAL`) |
| `tr` | Translate characters (`-d`, `-s`, `-c`) |
| `rev` | Reverse each line |
| `tac` | Print lines in reverse order |
| `nl` | Number lines (`-b`, `-n`, `-w`, `-s`) |
| `fold` | Wrap lines to width (`-w N`, `-s`) |
| `paste` | Merge file lines side by side (`-d DEL`, `-s`) |
| `column` | Columnate lists (`-t`, `-s SEP`) |
| `diff` | Compare two files (`-u`, `-q`) |
| `join` | Join sorted files on a common field (`-1`, `-2`, `-t`) |
| `comm` | Compare two sorted files line by line (`-1`, `-2`, `-3`) |
| `expand` | Convert tabs to spaces (`-t N`) |
| `unexpand` | Convert spaces to tabs (`-t N`, `-a`) |

**Data**

| Command | Description |
|---------|-------------|
| `jq` | JSON processor (`-r`, `-c`, `-e`, `-s`, `select`, `map`, `sort_by`, `add`, `length`, etc.) |

**Utilities**

| Command | Description |
|---------|-------------|
| `xargs` | Build commands from stdin (`-I REPL`, `-d DEL`, `-n N`, `-0`) |
| `seq` | Print number sequence (`-s SEP`, `-w`) |
| `printf` | Format and print data |
| `date` | Display date/time (`-u`, `+FORMAT`) |
| `basename` / `dirname` | Strip directory or last path component |
| `tee` | Pass stdin through to stdout |
| `base64` | Base64 encode/decode (`-d`) |
| `md5sum` / `sha1sum` / `sha256sum` | Compute checksums (`-c`) |
| `expr` | Evaluate arithmetic expressions |
| `which` / `type` | Locate or identify a command |
| `time` / `timeout` | Time a command or run with timeout |
| `whoami` / `hostname` | Print user/host info |
| `true` / `false` | Exit with 0 / 1 |
| `sleep` / `clear` | Sleep (stub) / clear screen |
| `command` | Run or locate a command (`-v`) |
| `version` | Print OpenLore version |

**Shell Builtins**

| Command | Description |
|---------|-------------|
| `echo` | Print text (`-n`, `-e` with escape sequences) |
| `export` | Set environment variables (`-p`) |
| `unset` | Remove variables |
| `env` / `printenv` | Print environment |
| `set` | Set or list shell variables (`--`) |
| `test` / `[` / `[[` | Conditional tests (`-f`, `-d`, `-e`, `-z`, `-n`, `=`, `!=`, `-eq`, `-lt`, etc.) |
| `read` | Read from stdin (`-p`, `-r`, `-a`, `-d`, `-n`) |
| `source` / `.` | Execute commands from a file |
| `eval` | Evaluate a string as a command |
| `help` | Show available commands |
| `skills` | List available skill commands |
| `exit` / `quit` | Close session |

**Publishing**

| Command | Description |
|---------|-------------|
| `publish` | Publish content from stdin to a docset (`echo "..." \| publish <docset> <path>`) |

**Shell Syntax**

| Feature | Example |
|---------|---------|
| Pipes | `grep pattern file \| sort \| head -5` |
| AND / OR | `test -f x && echo yes \|\| echo no` |
| Semicolons | `echo a; echo b` |
| Subshells | `(echo a; echo b)` |
| For loops | `for x in a b c; do echo $x; done` |
| If/else | `if test -f x; then cat x; else echo missing; fi` |
| While/until | `while test $i -lt 5; do echo $i; i=$(expr $i + 1); done` |
| Variables | `FOO=bar; echo $FOO` |
| Expansion | `${VAR:-default}`, `${VAR:+alt}`, `${#VAR}`, `$(cmd)` |
| Quoting | Single quotes preserve literal text, double quotes allow expansion |
| Negation | `! false` returns 0 |

### What's NOT Supported (By Design)

No `rm`, `mv`, `cp`, `chmod`, `wget`, `curl`, `bash -c`, or `exec` from a normal session. The shell is an interpreter, not a real bash process. The filesystem is read-only by default; when writing is enabled the only mutation surface is the whole-file write verbs (`write`, `>`, `>>`, `tee`, `patch`, `sed -i`), `mkdir` inside docsets, `publish`, and — for explicitly trusted identities — `spawn` (see [Writing](#writing)). There is no streaming, partial, or offset write anywhere.

## Skills

Skills are commands that output markdown to stdout. They're not files in the filesystem — they keep the docs filesystem clean while providing agent-facing instructions.

Built-in skills:
- `teach` — Setup instructions for OpenLore
- `agents` — AGENTS.md snippet for agent configuration

List all skills with the `skills` command. You can add custom skills by creating a `skills/` directory with a `skills.json` manifest.

## Publishing

Agents can publish content to writable docsets using the `publish` command:

```bash
# Publish from an interactive session
echo "# API Notes" | publish backend api-notes.md

# Publish remotely (non-interactive)
echo "# Research" | ssh -p 2222 server publish backend research/findings.md

# List writable docsets
ssh -p 2222 server publish
```

Enable publishing by adding `publish_dir` to a docset in your `lore.json`:

```json
{
  "docsets": {
    "backend": {
      "paths": ["/docs/backend"],
      "publish_dir": "./published/backend"
    }
  }
}
```

Published files are written to the `publish_dir` on disk. If the directory is within the served tree, files appear in the VFS immediately.

## Writing

`publish` is one of several **write verbs**. OpenLore can be a safe, writable
knowledge layer that agents and teammates share over SSH — read-only by default,
but with controlled, atomic, auditable mutation when you enable it. Every write
is a **whole-object atomic swap** (temp file → fsync → `rename(2)`); there is no
streaming, partial, or offset write.

```bash
echo "# Notes" > /mydocset/notes.md      # overwrite (compare-and-swap by default)
echo "- point" >> /mydocset/notes.md     # safe concurrent append
cat input.md | tee /mydocset/copy.md     # write stdin to a file
cat change.diff | patch /mydocset/x.md   # apply a unified diff atomically
sed -i 's/old/new/g' /mydocset/x.md      # edit in place
mkdir /mydocset/section                   # create a folder inside a docset
echo "# API" | publish mydocset api.md   # publish a new source
```

Key properties:

- **Read-only by default.** The substrate boots read-only; writes require
  `readonly: false` (or per-identity write scope). Embedded-docs binaries can
  never be made writable.
- **Scoped per identity.** A session can only write the docsets it's allowed to
  `publish` to — everyone can read the shared lore, but each agent writes only
  its own space.
- **Compare-and-swap by default.** Overwrites are rejected (not silently
  clobbered) if the file changed since you read it (`write_conflict_policy: hash`,
  overridable to `last_write_wins`). Append and `patch` are always CAS.
- **Optional human-in-the-loop approval.** A docset can mark paths
  `requires_approval`; a write to those becomes a pending request under
  `/requests` that an approver with the right capability commits via `approve`.
- **Async external work (`spawn`).** Trusted identities (granted the `spawn`
  capability) can run an external command and write its output back into the lore
  in the background; track it under `/jobs`. The write-back is scoped,
  CAS-checked, and approval-gated like any other write.

Enable and tune writing in `openlore.yml` / `lore.json`:

```yaml
readonly: false              # turn on the writable substrate
write_conflict_policy: hash  # hash (CAS, default) | last_write_wins
max_jobs: 8                  # bound concurrent async spawn jobs
```

```json
{
  "docsets": {
    "ops": {
      "paths": ["/ops"],
      "publish_dir": "./published/ops",
      "write_conflict_policy": "hash",
      "requires_approval": [
        { "path": "/ops/policy.md", "capability": "approve@oncall" }
      ]
    }
  }
}
```

For the full design and internals — the layered session filesystem, the single
write seam, preconditions, approvals, events/hooks, and async jobs — see
[`docs/write-system.md`](docs/write-system.md). Connected agents can read the
user-facing guide with `cat /writes.md`.

## CLI Commands

```
Usage: openlore [command] [flags] [directory]

Commands:
  version           Print version
  export -o <dir>   Export embedded docs to a directory
  mcp [dir]         Run as an MCP server over stdio (Claude Desktop, Cowork, etc.)
  mcpb [-o file]    Package the binary as an MCPB desktop extension
  identity add      Add a public key to lore.json

Flags:
  -p, --port           SSH server port (default 2222)
  --http-port          HTTP front page port (default 8080, 0 to disable)
  --mcp-path           MCP-over-HTTP endpoint path on the HTTP server (default /mcp)
  --metrics-port       Prometheus metrics port, 0 to disable (default 3000)
  --host-key           Path to host key file (default .ssh/openlore_ed25519)
  --motd               Inline MOTD string
  --motd-file          Path to MOTD file
  --auth               Path to lore.json
  -c, --config         Path to config file (default ./openlore.yml)
  --allowed            Comma-separated file patterns (e.g. '*.md,*.txt')
  --ignore             Comma-separated ignore patterns (e.g. '.git,node_modules')
  --tls-cert           TLS certificate file for HTTP server
  --tls-key            TLS key file for HTTP server
  --ca-keys            Trusted CA public keys for SSH certificate auth
  --host-cert          SSH host certificate signed by a CA
  --skills-dir         Directory containing runtime skills
```

## Agent Setup

Add your docs server to your agent's context:

```bash
# Add a directory listing to AGENTS.md
ssh -p 2222 localhost "tree -L 2 /" >> AGENTS.md

# Or use the agents skill
ssh -p 2222 localhost agents >> AGENTS.md
```

Or give the agent a tool instruction:

```markdown
## Documentation Access

Connect to the docs server for project documentation:

    ssh -p 2222 docs.internal "cat /api/endpoints.md"

Available commands: ls, cat, grep, find, tree, head, tail, wc, stat, sort, uniq, cut, sed, awk, jq, xargs, and more. Run 'help' for the full list.
```

## MCP Server

OpenLore can also speak the [Model Context Protocol](https://modelcontextprotocol.io)
so clients like Claude Desktop, Cowork, and other MCP-aware agents can browse
your docs without SSH. It exposes the same filesystem as the SSH shell via two
tools:

| Tool | Description |
|------|-------------|
| `shell` | Execute a bash command against the docs filesystem (`ls`, `cat`, `grep`, `find`, pipes, loops, and all the commands listed above) |
| `list_commands` | List all available shell commands |

### Always-on HTTP endpoint (default)

The MCP-over-HTTP endpoint (Streamable HTTP transport) is **on by default**,
mounted at a path on the HTTP server. Because it shares the HTTP port, it reuses
the same TLS and any load balancer rule already fronting the front page — no
extra port to open:

```bash
openlore ./docs
#   SSH:  ssh -p 2222 localhost
#   HTTP: http://localhost:8080
#   MCP:  http://localhost:8080/mcp
```

Point a Streamable-HTTP MCP client at `http://localhost:8080/mcp` (behind a
TLS-terminating proxy this is `https://your-host/mcp`). Configure it via
`openlore.yml`:

```yaml
mcp:
  enabled: true   # on by default; set false to disable
  path: /mcp      # path on the HTTP server
```

Or with flags: `--mcp-path /custom` to change the path. The endpoint requires
the HTTP server (`--http-port`) to be enabled.

### Stdio (Claude Desktop, Cowork, etc.)

For clients that launch a local process and talk over stdio, run the dedicated
`mcp` subcommand instead:

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

### Package as a desktop extension (.mcpb)

For one-click installation in Claude Desktop, package the binary (with its
embedded docs) as an [MCPB](https://github.com/anthropics/mcpb) extension:

```bash
# Build a binary with your docs embedded, then package it
go build -o openlore ./cmd/openlore
./openlore mcpb -o openlore.mcpb
```

Double-click the resulting `.mcpb` file (or drag it into Claude Desktop) to
install. If the binary has no embedded docs, the user is prompted to select a
docs directory on install. Add `--docs-dir ./docs` to bundle a directory.

### As a library

Build an MCP server backed by any filesystem in Go:

```go
package main

import (
    "context"

    openlore "github.com/aakarim/go-openlore/pkg/openlore"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
    fs := openlore.NewDirFS("./docs", openlore.FilesConfig{
        Allowed: []string{"*.md", "*.txt"},
    })

    srv := openlore.NewMCPServer(fs,
        openlore.WithMCPServerName("Company Knowledge Base"),
        openlore.WithMCPInstructions("Use grep and cat to explore the docs."),
    )

    srv.Run(context.Background(), &mcp.StdioTransport{})
}
```

## SSHFS Mounting

Mount your docs as a local filesystem using SFTP:

```bash
# Mount
mkdir -p /mnt/docs
sshfs -p 2222 localhost:/ /mnt/docs -o ro

# Now use any tool
grep -r "API" /mnt/docs/
code /mnt/docs/

# Unmount
fusermount -u /mnt/docs  # Linux
umount /mnt/docs          # macOS
```

## Configuration

### openlore.yml

Create an `openlore.yml` in your project root (or pass `--config path/to/config.yml`):

```yaml
version: "1"

port: 2222
metrics_port: 3000
http_port: 8080
host_key: .ssh/openlore_ed25519
allow_keyless: true
default_cwd: /docs

# MCP-over-HTTP endpoint (on by default). Set enabled: false to disable.
mcp:
  enabled: true
  path: /mcp

motd: |
  Welcome to Acme Corp docs.
  Type 'tree -L 1 /' to get started.

files:
  allowed:
    - "*.md"
    - "*.txt"
    - "*.yml"
    - "*.json"
  ignore:
    - ".git"
    - "node_modules"
    - ".env"

# skills_dir: ./skills
# auth_file: ./lore.json
# tls_cert: ./cert.pem
# tls_key: ./key.pem
```

## Identity & Auth

### Keyless (Default)

By default, any SSH client can connect. No keys required. To require public key auth, set `"allow_keyless": false` in your `lore.json`.

### Public Key Auth

Create a `lore.json` to control access per public key:

```json
{
  "allow_keyless": true,
  "unknown_identity": "allow",
  "default_cwd": "/docs",
  "lore": {
    "default": { "paths": ["/docs/public"] },
    "backend": {
      "paths": ["/docs/api", {"internal/specs": "/docs/specs"}]
    },
    "full-access": { "paths": ["/"] }
  },
  "identities": [
    {
      "name": "backend-agent",
      "public_key": "ssh-ed25519 AAAA...",
      "lore": "backend"
    }
  ]
}
```

### Managing Identities

```bash
openlore identity add \
  --name my-agent \
  --key "ssh-ed25519 AAAA..." \
  --lore backend \
  --auth ./lore.json
```

### Unknown Identity Handling

In `lore.json`:
- `"unknown_identity": "allow"` (default) — unrecognized keys get the "default" lore spec
- `"unknown_identity": "deny"` — reject unrecognized keys

## HTTP Front Page

A human-facing web page is served on port 8080 by default. Disable it with `--http-port 0`:

```bash
openlore ./docs                   # HTTP on :8080 (default)
openlore --http-port 3000 ./docs  # HTTP on :3000
openlore --http-port 0 ./docs     # HTTP disabled
```

The front page includes an **SSH Host Key** section that displays the server's public key and a ready-to-paste `known_hosts` entry. If you serve the HTTP page over TLS (`--tls-cert` / `--tls-key`), this gives clients a trusted way to verify the server's SSH identity before their first connection.

### Verifying the Host Key

SSH doesn't have public certificate authorities like TLS does — there is no Let's Encrypt for SSH. When a client connects for the first time, it has to trust the server's key on first use (TOFU), which is vulnerable to man-in-the-middle attacks.

OpenLore addresses this by exposing the host public key at `GET /host-key` on the HTTP server. **We recommend you serve the HTTP page over TLS** so that clients can:

1. Visit `https://your-server:8080` and verify the host key
2. Copy the `known_hosts` entry from the page (or `curl https://your-server:8080/host-key`)
3. Connect via SSH with confidence that they're talking to the real server

```bash
# Fetch the host key over HTTPS and add to known_hosts
curl -s https://docs.example.com/host-key | \
  awk '{print "[docs.example.com]:2222 " $0}' >> ~/.ssh/known_hosts

# Now connect — no TOFU prompt
ssh -p 2222 docs.example.com
```

If you use SSH certificate auth (`--ca-keys`, `--host-cert`), the host certificate provides even stronger guarantees. But for most deployments, publishing the host key over TLS is the simplest path to verified server identity.

See `examples/` for Caddy reverse proxy configurations.

## Bundling Docs into the Binary

Place your documentation files in `assets/lore/` and build:

```bash
go build ./cmd/openlore
```

The binary now contains your docs. Run it without arguments and they're served at `/docs` over SSH.

## As a Library

```go
package main

import (
    "log"
    openlore "github.com/aakarim/go-openlore/pkg/openlore"
)

func main() {
    srv, err := openlore.NewServer("./docs",
        openlore.WithPort(2222),
        openlore.WithHTTPPort(8080),
        openlore.WithAllowedPatterns([]string{"*.md", "*.txt"}),
    )
    if err != nil {
        log.Fatal(err)
    }

    srv.OnConnect(func(id openlore.Identity) {
        log.Printf("connected: %s from %s", id.User, id.RemoteAddr)
    })

    log.Printf("serving docs on :2222")
    if err := srv.ListenAndServe(); err != nil {
        log.Fatal(err)
    }
}
```

## Security

OpenLore is designed to be safe to expose on a network:

- **Controlled writes** — the `publish` command is the only write path, restricted to docsets with `publish_dir` configured. No process execution, no network access from the shell
- **In-memory bash** — commands are interpreted as pure Go functions, not executed via `os/exec`
- **No shell injection** — command parsing is structural, not string interpolation
- **File type filtering** — only serve files matching allowed patterns
- **Directory ignoring** — skip `.git`, `node_modules`, `.env`, and other sensitive paths
- **Path traversal protection** — all paths are cleaned and resolved within the VFS root
- **Host key verification** — the HTTP front page displays the SSH host key and serves it at `/host-key`. When the HTTP server is TLS-secured, this gives clients an independently verifiable trust anchor for the SSH connection. SSH has no public CA infrastructure, so we recommend verifying the host key over HTTPS before connecting.
- **SSH certificate auth** — supports CA-signed user certificates (`--ca-keys`) and host certificates (`--host-cert`) for environments that run their own SSH CA

See [SECURITY.md](SECURITY.md) for a full security evaluation.

## License

[MIT](LICENSE) — Adil Karim
