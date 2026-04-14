# 📜 OpenLore

**Serve your docs to AI agents over SSH.**

---

## The Problem

AI coding agents — Claude, GPT, Cursor, Codex — are trained on bash. They explore codebases with `ls`, `cat`, `grep`, and `find`. It's their native interface.

But when they need your documentation, they're stuck with fragile MCP servers, RAG pipelines, or copy-pasting into context windows. These approaches are complex to set up, hard to debug, and add layers of abstraction between the agent and the content.

## The Solution

OpenLore gives agents the same interface they already know — **a bash shell over SSH** — but serving your docs instead of a real filesystem.

It's a single binary, zero-config, read-only SSH server backed by an in-memory bash interpreter. No real processes. No shell injection. No escapes.

```
Agent → SSH → OpenLore → Your Docs
```

## Quick Start

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

## Teach Your Agent

Pipe the `teach` skill directly to your agent to get started:

```bash
# Teach your agent how to set up OpenLore
ssh openlore.sh teach | your-agent-cli

# Add documentation access instructions to AGENTS.md
ssh openlore.sh agents >> AGENTS.md
```

The `teach` skill walks your agent through cloning the repo, embedding docs, building a distributable binary, and optionally setting up per-agent access control.

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

No `rm`, `mv`, `cp`, `chmod`, `wget`, `curl`, `bash -c`, `exec`, or anything that writes, spawns processes, or accesses the network. The filesystem is read-only and the shell is an interpreter, not a real bash process.

## Skills

Skills are commands that output markdown to stdout. They're not files in the filesystem — they keep the docs filesystem clean while providing agent-facing instructions.

Built-in skills:
- `teach` — Setup instructions for OpenLore
- `agents` — AGENTS.md snippet for agent configuration

List all skills with the `skills` command. You can add custom skills by creating a `skills/` directory with a `skills.json` manifest.

## CLI Commands

```
Usage: openlore [command] [flags] [directory]

Commands:
  version           Print version
  export -o <dir>   Export embedded docs to a directory
  identity add      Add a public key to lore.json

Flags:
  -p, --port           SSH server port (default 2222)
  --http-port          HTTP front page port (0 to disable)
  --metrics-port       Prometheus metrics port, 0 to disable (default 3000)
  --host-key           Path to host key file (default .ssh/openlore_ed25519)
  --allow-keyless      Allow connections without SSH keys (default true)
  --motd               Inline MOTD string
  --motd-file          Path to MOTD file
  --auth               Path to lore.json
  -c, --config         Path to config file (default ./openlore.yml)
  --allowed            Comma-separated file patterns (e.g. '*.md,*.txt')
  --ignore             Comma-separated ignore patterns (e.g. '.git,node_modules')
  --tls-cert           TLS certificate file for HTTP server
  --tls-key            TLS key file for HTTP server
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

By default, any SSH client can connect. No keys required.

### Public Key Auth

Create a `lore.json` to control access per public key:

```json
{
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

In `openlore.yml`:
- `unknown_identity: allow` (default) — unrecognized keys get the "default" lore spec
- `unknown_identity: deny` — reject unrecognized keys

## HTTP Front Page

Enable a human-facing web page with `--http-port`:

```bash
openlore --http-port 8080 ./docs
```

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

- **Read-only** — no writes, no process execution, no network access from the shell
- **In-memory bash** — commands are interpreted as pure Go functions, not executed via `os/exec`
- **No shell injection** — command parsing is structural, not string interpolation
- **File type filtering** — only serve files matching allowed patterns
- **Directory ignoring** — skip `.git`, `node_modules`, `.env`, and other sensitive paths
- **Path traversal protection** — all paths are cleaned and resolved within the VFS root

See [SECURITY.md](SECURITY.md) for a full security evaluation.

## License

[MIT](LICENSE) — Adil Karim
