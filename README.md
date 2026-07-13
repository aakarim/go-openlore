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

**Introspection**

| Command | Description |
|---------|-------------|
| `whoami` | Print your identity |
| `lore` | Introspection dispatcher (run `lore` for subcommands) |
| `lore docsets` | List the docsets you can access, their grant, paths, and attributes |
| `lore meta` | Emit each document's frontmatter as NDJSON, cwd-scoped (see [Plugins](#plugins)) |

`lore docsets` prints an aligned, greppable table:

```
$ lore docsets
DOCSET    GRANT    ATTRIBUTES   PATHS
public    ro       -            /docs/public,/docs/getting-started.md
backend   rw       -            /docs/backend,/docs/api
home      rw       home,inbox   /home/backend
```

- `GRANT` is the named grant you hold on the docset: `ro` (read the whole docset),
  `rw` (read + write anywhere in it), or `publish` (read the whole docset, create/edit
  only within its inbox, never delete).
- `ATTRIBUTES` is a comma-joined set of tokens (`-` if none): `home` (your `$HOME`
  docset), `inbox` (the docset declares an inbox folder).

**Publishing**

| Command | Description |
|---------|-------------|
| `publish` | Publish content from stdin to a docset inbox (`echo "..." \| publish <docset> <path>`) |

`publish` targets a docset's **inbox** folder — the write surface a `publish` grant
confines create/edit to. `lore docsets` surfaces only the *presence* of an inbox (the
`inbox` attribute); run `publish` with no args to list your inboxes.

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

No `mv`, `cp`, `chmod`, `wget`, `curl`, `bash -c`, or `exec` from a normal session. The shell is an interpreter, not a real bash process. The filesystem is read-only by default; when writing is enabled the only mutation surface is the whole-file write verbs (`write`, `>`, `>>`, `tee`, `patch`, `sed -i`), `mkdir` / `mkdir -p` inside docsets, `rm` / `rm -r` inside docsets, `publish`, and — for explicitly trusted identities — `spawn` (see [Writing](#writing)). There is no streaming, partial, or offset write anywhere.

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

Enable publishing by giving a docset an `inbox` folder in your `lore.json` and
granting an identity `publish` (or `rw`) on it:

```json
{
  "docsets": {
    "backend": {
      "paths": ["/docs/backend"],
      "inbox": "inbox"
    }
  },
  "identities": [
    { "name": "contributor", "docsets": { "backend": "publish" } }
  ]
}
```

A `publish` grant lets the identity read the whole docset but only create/edit
files within its `inbox` folder (here `/docs/backend/inbox`) — never delete. An
`rw` grant can write anywhere in the docset. Published files appear in the VFS
immediately.

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
mkdir -p /mydocset/a/b/c                  # create nested folders
rm /mydocset/old.md                       # delete a file
rm -r /mydocset/section                   # delete a folder tree (atomic)
echo "# API" | publish mydocset api.md   # publish a new source
```

Key properties:

- **Read-only by default.** The substrate boots read-only; writes require
  `readonly: false` (or per-identity write scope). Embedded-docs binaries can
  never be made writable.
- **Scoped per identity.** Every write is authorized against the identity's
  per-docset grant — an `rw` grant writes anywhere in its docset, a `publish`
  grant only within the docset's inbox — so agents sharing a docset can't write
  each other's space.
- **Compare-and-swap by default.** Overwrites are rejected (not silently
  clobbered) if the file changed since you read it (`write_conflict_policy: hash`,
  overridable to `last_write_wins`). Append and `patch` are always CAS.
- **Optional human-in-the-loop approval.** A docset can mark paths
  `requires_approval`; a write or delete to those becomes a pending **changeset**
  under `/requests` that an approver with the right capability commits via
  `approve`. Deletes are captured as an exact subtree snapshot and stay live for
  review until approved.
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
      "inbox": "inbox",
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

## Plugins

OpenLore's write/read paths and command surface are extensible via **plugins** —
Go values registered with the server that are capability-detected at
registration. A plugin implements one or more provider interfaces:

| Interface | Contributes |
|---|---|
| `WriteMiddlewareProvider` | admission (pre-commit) middleware |
| `ReadMiddlewareProvider` | before-read middleware |
| `PostCommitProvider` | post-commit middleware |
| `GrantTypeProvider` | named grant types (e.g. `publish`) |
| `CommandProvider` | `lore` subcommands (`LoreCommands() []cmds.LoreSub`) |
| `MetaExtenderProvider` | fields added to `lore meta` records |
| `PluginInfoProvider` | plugin name + version (`Info() PluginInfo`), logged at boot |

Built-in plugins (`shellexec`, `inbox`, `okf`) are wired from config; consumers
add their own via `Server.RegisterPlugin`. A plugin can extend the introspection
surface without the `lore` dispatcher knowing about it: `CommandProvider` adds
whole subcommands, and `MetaExtenderProvider` enriches an existing command's
output (the okf plugin uses it to annotate `lore meta` — see below).

Every built-in plugin reports a name and semantic version via
`PluginInfoProvider`, recorded in the server's boot logs as it registers, so it
is always clear which plugins — and which versions — are active:

```
INFO plugin registered name=shellexec version=1.0.0
INFO plugin registered name=okf version=0.1.0
INFO plugin registered name=inbox version=1.0.0
```

The `okf` version tracks the OKF spec revision it validates (OKF v0.1).

### OKF Validator

The built-in **Open Knowledge Format** plugin validates knowledge documents on
write against [OKF v0.1](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md).
OKF is a directory of markdown files with YAML frontmatter; a document is
conformant when every non-reserved `.md` file opens with a parseable frontmatter
block containing a non-empty `type` field. Reserved files (`index.md`,
`log.md`) are validated leniently.

Enable it **per docset** in `lore.json` by adding an `okf` block to a docset —
so OKF scoping reads the same display roots as the docset's paths and grants and
can never drift from them. It runs as pre-commit admission middleware, so a
non-conformant write to that docset's subtree (via `>`, `tee`, `patch`, `sed
-i`, `publish`, `spawn`, or any verb funneling through the write log) is
**rejected before it hits disk**:

```json
{
  "docsets": {
    "wiki": {
      "paths": ["/wiki"],
      "okf": {
        "patterns": ["*.md"],
        "enforce": true
      }
    }
  }
}
```

A write is governed by the OKF config of the docset that **owns** its target
path — the longest matching display root, exactly as grants resolve. Scope
narrower subtrees with **nested docsets**: a child docset that carries `okf`
adds validation to that subtree; a child docset without `okf` shadows a parent's
OKF and exempts that subtree. For example, make `/adil` a docset with no `okf`
and `/adil/wiki` a nested docset with `okf` to enforce OKF only under
`/adil/wiki` while leaving the rest of `/adil` untouched. `patterns` defaults to
`["*.md"]` and `enforce` defaults to `true` (set `false` to log and allow).

The same validation logic is a dependency-light library at
[`pkg/okf`](pkg/okf), so downstream tooling (e.g. a `kb save`/`kb publish`
command) can import `okf.Validate` / `okf.ParseFrontmatter` and enforce
identical conformance without going through the write path:

```go
import "github.com/aakarim/go-openlore/pkg/okf"

if err := okf.Validate(path, content); err != nil {
    // not OKF-conformant
}
```

### `lore meta` — frontmatter reader

`lore meta` is a read-side, cwd-scoped introspection command on the `lore`
dispatcher. It walks documents from the current directory (or an optional path
argument, like `find [path]`) and emits **each document's YAML frontmatter as
NDJSON** — one JSON object per line, `path` relative to where you are. It is a
generic *reader*, not a validator: it emits any `*.md` that opens with a
parseable frontmatter block (skipping those that don't) and passes through the
full frontmatter map, so `jq` can reach any producer-defined field. Bodies stay
out — that's the token win; use `cat`/`grep` to drill in.

```bash
cd backend
lore meta                                                    # frontmatter for backend/**
lore meta | jq -r .type | sort -u                            # what document types exist?
lore meta | jq -r 'select(.type=="Metric").path' | xargs cat # drill into metrics
```

Read-scoping comes for free: the walk goes through the session filesystem, so
`lore meta` only ever sees what the identity can already read.

**OKF annotation.** When the okf plugin is active, it enriches `lore meta`
records (via `MetaExtenderProvider`) with an `okf` field — but only for
documents where OKF actually applies (the owning docset has `okf` and the path
matches its patterns), so read-side discovery agrees exactly with write-side
enforcement:

```json
{"path":"orders.md","type":"Table","okf":{"valid":true}}
{"path":"draft.md","title":"No type","okf":{"valid":false,"error":"frontmatter is missing the required non-empty 'type' field"}}
```

```bash
lore meta | jq -r 'select(.okf.valid == false) | .path'   # find non-conformant docs
```

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
  --log-unsupported    Log unsupported shell commands and flags as structured events
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
  "docsets": {
    "public": { "paths": ["/docs/public"] },
    "backend": {
      "paths": ["/docs/api", {"internal/specs": "/docs/specs"}]
    }
  },
  "default": { "public": "ro" },
  "identities": [
    {
      "name": "backend-agent",
      "public_key": "ssh-ed25519 AAAA...",
      "docsets": { "public": "ro", "backend": "rw" },
      "home": "backend"
    }
  ]
}
```

Each identity holds a named **grant** on each docset it can access — `ro`
(read), `rw` (read + write), or `publish` (read all, create/edit only in the
docset's inbox, no deletes). The `default` map is the grant set for keyless /
unrecognized callers.

### Home Directory

An identity can name one of its granted docsets as its `home`. That docset's
display path becomes the session's `$HOME`, enabling `~` and `~/path` expansion
and letting `cd` with no arguments jump home. The session still starts in the
default working directory (`default_cwd`) — `home` only sets `$HOME`, not where
you land on connect:

```json
{
  "name": "backend-agent",
  "public_key": "ssh-ed25519 AAAA...",
  "docsets": { "public": "ro", "backend-home": "rw" },
  "home": "backend-home"
}
```

```bash
ssh -p 2222 server 'echo $HOME'   # -> /home/backend (the home docset's path)
ssh -p 2222 server "cat ~/notes.md"
ssh -p 2222 server "cd && pwd"    # -> /home/backend
```

The `home` docset must be one of the identity's granted docsets. Give it an
`inbox` (with an `rw` grant) to hand each agent its own writable personal space.

### Managing Identities

```bash
openlore identity add \
  --name my-agent \
  --key "ssh-ed25519 AAAA..." \
  --docset backend \
  --grant rw \
  --home backend \
  --auth ./lore.json
```

The `--key` flag is optional: an identity can exist as a passkey/token-only
login target with no SSH key.

### Unknown Identity Handling

In `lore.json`:
- `"unknown_identity": "allow"` (default) — unrecognized keys get the `default` grant map
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

OpenLore bundles third-party open-source components. Their licenses and required
notices are listed in
[assets/legal/THIRD_PARTY_NOTICES.md](assets/legal/THIRD_PARTY_NOTICES.md), with
full license texts in [assets/legal/licenses/](assets/legal/licenses/). These are
embedded in the binary and served by the running service at `/legal`.
