# Security Evaluation

This document describes the security properties of OpenLore and the design decisions behind them.

## Threat Model

OpenLore is a **read-only SSH server** that serves a virtual filesystem. It is designed to be safe to expose on internal networks and, with public key auth enabled, on the public internet. The primary threat actors are:

1. **Malicious SSH clients** attempting to escape the sandbox
2. **Agents or users** attempting to access files outside their allowed scope
3. **Denial-of-service** attacks against the SSH server

## Architecture Layers

### 1. Command Parser (`splitArgs`)

Commands are parsed using a structural tokenizer, not by passing strings to a shell. There is no shell interpolation, no backtick expansion, no `$(...)` substitution, and no glob expansion by the parser.

**No shell injection is possible** because:
- Commands are tokenized into `[]string` arguments
- Each command name is matched against a fixed allowlist of built-in functions
- Unknown commands return an error — they are never passed to `os/exec` or any system shell
- Pipe chains are handled by splitting on `|` and connecting in-memory buffers

### 2. Virtual Filesystem (VFS)

The VFS layer provides a read-only view over an `fs.FS` (either `os.DirFS` or `embed.FS`).

**Path traversal protection:**
- All paths are processed through `path.Clean` to normalize `.` and `..` components
- Resolved paths are checked to ensure they remain within the VFS root
- Symlinks pointing outside the root are not followed

**Read-only enforcement:**
- The VFS interface only exposes `Open`, `ReadDir`, and `Stat`
- No write, rename, delete, or permission-change operations exist in the interface
- File handles returned are read-only

**File filtering:**
- Allowed patterns (e.g., `*.md`, `*.txt`) restrict which files are visible
- Ignore patterns (e.g., `.git`, `.env`) hide directories and files entirely
- Filtering is applied at the VFS level — filtered files don't appear in `ls`, `find`, `tree`, or `cat`

### 3. SSH Transport

SSH transport is handled by [charmbracelet/ssh](https://github.com/charmbracelet/ssh), which wraps [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh).

**Properties:**
- Standard SSH key exchange and encryption (curve25519-sha256, aes256-gcm, etc.)
- Host key identity via ed25519 key (auto-generated on first run)
- Public key authentication validated by `golang.org/x/crypto/ssh` key parsing
- No password authentication (even in keyless mode — the server accepts all keys, it doesn't use passwords)

### 4. SFTP Subsystem

The SFTP server provides read-only file access for `sshfs` mounting.

**Read-only enforcement:**
- Write operations (`Create`, `Remove`, `Rename`, `Mkdir`, `Chmod`, etc.) return `os.ErrPermission`
- Only `Open` (read), `ReadDir`, and `Stat` are functional
- The SFTP handler wraps the same VFS used by the shell, so file filtering and path protection apply

### 5. In-Memory Bash Interpreter

The bash interpreter is **not** bash. It is a set of Go functions that implement common read-only Unix commands:

| Command | Implementation |
|---------|---------------|
| `ls`    | Calls `fs.ReadDir` on the VFS |
| `cat`   | Calls `fs.Open` and reads the file |
| `grep`  | Compiles a `regexp.Regexp` and scans lines |
| `find`  | Walks the VFS with `fs.WalkDir` |
| `tree`  | Recursive `fs.ReadDir` with formatting |
| `head`/`tail` | Reads lines from a file buffer |
| `wc`    | Counts bytes/lines in a buffer |
| `stat`  | Calls `fs.Stat` on the VFS |
| `cd`/`pwd` | Tracks working directory in session state |

**No process execution:**
- No use of `os/exec`, `syscall.Exec`, or any process spawning
- `bash`, `sh`, `/bin/sh`, `exec`, and similar commands are not recognized
- Environment variables are not expanded (no `$HOME`, `$PATH`, etc.)

### 6. Identity & Auth

**Keyless mode (default):**
- All SSH connections are accepted regardless of key
- The user field from the SSH session is available but not enforced
- Suitable for local development and trusted networks

**Public key mode:**
- Public keys in `auth.json` are matched against the client's presented key
- Per-identity folder restrictions limit which VFS paths are accessible
- Keys are validated by `golang.org/x/crypto/ssh.ParseAuthorizedKey`
- Unknown keys are rejected at the SSH handshake level

**Certificate authority mode (user certificates):**
- When `ca_keys_file` is configured, the server accepts SSH user certificates signed by trusted CA keys
- Analogous to OpenSSH's `TrustedUserCAKeys` directive
- The CA's public key is stored on the server; users present certificates signed by that CA
- Certificate validity (expiry, principals) is enforced by `golang.org/x/crypto/ssh`
- Can be combined with public key auth — certificates and raw keys are both accepted

**Host certificates (server identity):**
- When `host_cert_file` is configured, the server presents a CA-signed host certificate to clients
- Analogous to OpenSSH's `HostCertificate` directive
- Clients configure `@cert-authority` in `known_hosts` to trust all hosts signed by the CA
- **Limitation: still relies on TOFU for CA trust distribution.** The client must obtain and configure the CA public key *before* the first connection. If an attacker intercepts the first connection before the client has the CA key, they can present a fraudulent server. SSH has no equivalent of the TLS WebPKI or Certificate Transparency — there is no global, independently-auditable registry of host CA keys. In practice, this means:
  - The CA public key must be distributed through a trusted out-of-band channel (e.g., internal wiki, config management, signed package, or a well-known HTTPS endpoint)
  - Publishing the CA public key at a stable, publicly-accessible HTTPS URL (e.g., `https://example.com/.well-known/ssh-ca.pub`) and documenting it in your README is strongly recommended. HTTPS provides the independent trust anchor that SSH lacks
  - For automated agent deployments, bake the CA public key into the agent's image or configuration rather than relying on first-connection trust

### 7. Denial of Service

OpenLore does **not** include built-in rate limiting or connection limits. This is by design — these concerns are better handled at the infrastructure level:

- Use a reverse proxy or load balancer for connection rate limiting
- Use OS-level limits (`ulimit`, `systemd` resource controls) for per-process constraints
- Use firewall rules to restrict source IPs

**Resource considerations:**
- Each session maintains an in-memory working directory path (minimal memory)
- File reads are streaming (not buffered entirely in memory)
- `grep -r` and `find` on large filesystems will consume CPU proportional to the number of files
- The VFS does not cache file contents — each read goes to disk (or embed.FS)

## Known Limitations

1. **No rate limiting** — must be handled externally
2. **No TLS termination** — not needed, SSH provides encryption
3. **No audit logging** — connection events are logged via slog, but no detailed command audit trail is built in (use the `OnConnect` hook in library mode for custom auditing)
4. **Symlink handling** — symlinks within the served directory are followed; symlinks pointing outside are not. A determined attacker with control over the served directory could create symlinks to sensitive files. Only serve directories you trust.
5. **Large file reads** — `cat` on a very large file will stream the entire content. There is no built-in size limit. Consider using `head` for large files.
6. **No built-in CA key distribution** — SSH host certificates shift trust from individual host keys to a CA, but clients must still obtain the CA public key out-of-band before the first connection. Unlike TLS (where browsers ship with trusted root CAs), SSH has no pre-installed trust store. Publish your CA public key at a well-known HTTPS URL so clients can fetch and verify it independently.

## Recommendations

- **Enable public key auth** in production: set `allow_keyless: false` and configure `auth.json`
- **Use file filtering** to avoid serving sensitive file types
- **Use ignore patterns** to exclude `.git`, `.env`, `node_modules`, and other non-doc content
- **Run behind a firewall** or VPN for internal documentation servers
- **Use `go:embed`** for public-facing docs to eliminate filesystem access entirely
- **Publish your SSH CA public key over HTTPS** if using host certificates. Host it at a stable URL (e.g., `https://example.com/.well-known/ssh-ca.pub`) so clients can verify the CA independently before their first SSH connection. This closes the TOFU gap that SSH certificates alone cannot solve.
