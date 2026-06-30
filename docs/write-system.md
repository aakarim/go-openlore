# OpenLore Write System — Internals

This document describes exactly how OpenLore turns its read-only virtual
filesystem into a **safe, writable** one for coding agents and humans, and how
the pieces fit together if you want to dig into the source.

OpenLore started life as a read-only SSH file server for documentation. The
write system layers controlled, auditable mutation on top of that substrate
**without** giving anyone a real shell, a real process, or unscoped disk access.
Every write is a whole-object, atomic swap that flows through the same narrow
seam, so concurrency, scoping, approval, and notification all compose cleanly.

> TL;DR: a write is `WriteFileAtomic(path, bytes, precondition)`. There is no
> streaming, no partial write, no offset, no `open()`/`fsync()`/`close()` for
> callers. The substrate writes a temp file, fsyncs it, and `rename(2)`s it into
> place under a lock that also checks the precondition.

---

## 1. Design principles

1. **Whole-object, atomic, no streams.** The only mutating primitive is
   `WriteFileAtomic`: it takes the complete new contents and commits them in one
   `rename(2)`. There is no way to observe or persist a half-written file. Even
   "append" and "patch" are implemented as read-modify-write of the whole file.
2. **One write seam.** Every write verb (`>`, `>>`, `tee`, `sed -i`, `patch`,
   `publish`, and the async `spawn` write-back) funnels through one helper
   (`cmds.WriteFile` / `cmds.WriteFileCAS`). Concurrency control, scoping, and
   approval are enforced once, at that seam — not re-implemented per command.
3. **Read-only by default.** The substrate boots read-only. Writes require an
   explicit, stateful flip (`SetWriteable`). Embedded (`embed.FS`) builds can
   never be made writable — the capability is physically absent.
4. **Least authority per session.** A session can only write the docsets its
   identity is scoped to, and only the verbs its capabilities allow. An
   anonymous/read-only session can't even *see* the write surface.
5. **Optimistic concurrency by default.** Overwrites are compare-and-swap (CAS)
   against the bytes the caller read, so a concurrent change is rejected, not
   silently clobbered.

---

## 2. The substrate: `vfs.WritableFS`

The contract lives in [`pkg/vfs/vfs.go`](../pkg/vfs/vfs.go). The read-only base
is `vfs.FileSystem` (`Stat`, `ReadDir`, `ReadFile`). A backend that can persist
writes additionally implements `vfs.WritableFS`:

```go
type WritableFS interface {
    FileSystem
    SetWriteable() error
    SetReadonly() error
    WriteFileAtomic(name string, data []byte, opts WriteOpts) (newHash string, err error)
    Mkdir(name string) error
}
```

Key points:

- **`SetWriteable` / `SetReadonly` are a stateful flag.** While read-only, every
  mutating call returns `vfs.ErrReadOnly`. `SetReadonly` is *draining*: a write
  that already passed its precondition check is allowed to finish, and
  `SetReadonly` blocks until the substrate is quiescent. This is what makes a
  graceful shutdown / read-only flip safe under load.
- **Embedded backends deliberately don't implement `WritableFS`.** A binary that
  embeds its docs via `embed.FS` is read-only by construction — there is no disk
  to write to and no `SetWriteable` to call.
- **`WriteFileAtomic` returns the new content hash** (hex SHA-256), which callers
  use to chain CAS writes.

### The on-disk implementation: `DirFS`

[`pkg/openlore/vfs.go`](../pkg/openlore/vfs.go) implements `DirFS`, the writable
backend over a real directory. `WriteFileAtomic`:

1. Rejects writes over the size limit, denied filenames, or ignored paths.
2. Takes `stateMu.RLock()` for the whole mutation. Multiple writers hold the
   read lock concurrently; `SetReadonly` takes the exclusive lock, so it blocks
   until all in-flight writers drain. If `!writeable`, returns `ErrReadOnly`.
3. Takes `commitMu.Lock()` so the **precondition check and the swap are atomic**
   with respect to other writers to the same `DirFS`.
4. If a precondition is set, computes the current file hash and compares
   (`IfMatch`) or asserts non-existence (`IfNoneMatch`); mismatch →
   `vfs.PreconditionError`.
5. Commits via `atomicWrite`: write to a temp file in the destination dir,
   `fsync`, `chmod 0644`, then `os.Rename` into place (POSIX atomic swap).
6. Publishes a `post_write` event on the eventbus (if wired).

`Mkdir` uses plain mkdir semantics (parent must exist) but **refuses to create a
docset root or anything at/above one** — you can only create folders strictly
*inside* a docset.

### `MergeFS`: routing + control-plane mounts

`MergeFS` (same file) composes the served tree: a root FS plus named mounts. It
routes `WriteFileAtomic`/`Mkdir` to the backend owning the resolved path, and
fans `SetWriteable`/`SetReadonly` out to every writable-capable backend. It also
hosts **system mounts** (`MountSystem`) for the computed control-plane
filesystems `/requests` and `/jobs`, which survive a session's `FilteredView`.

---

## 3. Preconditions & the conflict policy

### `WriteOpts` — the precondition contract

```go
type WriteOpts struct {
    IfMatch     *string // require current hash == *IfMatch (CAS)
    IfNoneMatch bool    // require the target to not exist (create-only)
}
```

The zero value is an **unconditional atomic overwrite** (last-write-wins, but
still atomic). `IfMatch` carries the hex SHA-256 of the exact bytes the caller
read, giving optimistic concurrency. A failed precondition returns
`vfs.PreconditionError` carrying the *current* hash so the caller can re-read and
retry.

### `WriteConflictPolicy` — how overwrites defend themselves

Configured globally (`write_conflict_policy`) and overridable per docset:

- **`hash` (default)** — overwrites are compare-and-swap. The write seam reads
  the current bytes at command time and sends them as the `IfMatch` base, so a
  change made since the read is rejected with a `PreconditionError`. For a
  read-modify-write verb (`sed -i`, `patch`) the base is the exact content it
  transformed, giving *true* optimistic concurrency; for a blind redirect
  (`echo … > file`) the base is read at command time, so the guarantee narrows to
  the command's own read→write window.
- **`last_write_wins`** — overwrites are unconditional (zero `WriteOpts`):
  atomic, but the last writer silently wins.

**Append (`>>`) and `patch` are always CAS**, regardless of policy. Append runs a
read-modify-write retry loop (`cmds.WriteFile` with `appendMode`): read current,
compute hash, append, commit-if-unchanged, and on `PreconditionError` re-read and
retry (bounded), so concurrent appends never clobber each other.

---

## 4. The single write seam: `cmds.WriteFile`

[`pkg/shell/cmds/write.go`](../pkg/shell/cmds/write.go) is where every write verb
converges:

- `WriteFile(ctx, path, data, appendMode)` — blind overwrite (policy-governed
  CAS or unconditional) or atomic append (always CAS loop).
- `WriteFileCAS(ctx, path, data, base)` — overwrite where the caller already
  holds the base it transformed (`sed -i`, `patch`); the precondition is the true
  base, not a re-read.
- `WriteFileMsg` / `WriteFileCASMsg` — the same, with uniform shell-style error
  messaging (read-only, precondition, generic) and exit codes.

Because all verbs go through here, the policy resolution, CAS base capture, and
read-only/precondition error reporting exist in exactly one place. Commands that
write: [`write`/`tee`/redirects](../pkg/shell/cmds/write.go),
[`patch`](../pkg/shell/cmds/patch.go), [`sed -i`](../pkg/shell/cmds/sed.go),
[`publish`](../pkg/shell/cmds/publish.go), and
[`spawn`](../pkg/shell/cmds/spawn.go)'s background write-back.

---

## 5. Per-session composition (the layered FS)

The interesting part is how a session's filesystem is *built up* in
[`pkg/openlore/server.go`](../pkg/openlore/server.go). Outermost first, a write
travels:

```diagram
  write verb (capability-gated by Action)
        │
        ▼
  scopedWriteFS   ── is the target inside THIS identity's docset roots?  ──no──▶ ErrReadOnly
        │ yes
        ▼
  approvalFS      ── does the target match a requires_approval rule?     ──yes─▶ record pending request,
        │ no                                                                     return PendingApprovalError
        ▼
  DirFS substrate ── precondition check + atomic temp-write + rename(2) ──▶ post_write event
```

- **`scopedWriteFS`** ([`scoped_write_fs.go`](../pkg/openlore/scoped_write_fs.go))
  confines a session to a fixed set of docset roots (Part B per-identity
  isolation). Reads pass straight through; a write only reaches the substrate if
  its target sits *strictly inside* one of the writable roots, else
  `ErrReadOnly`. This is how two agents that can both *see* a shared docset are
  still prevented from writing each other's private docsets. It also implements
  `vfs.WriteScopeFS.CanWrite` for fail-fast checks (used by `spawn`).
- **`approvalFS`** ([`approval_fs.go`](../pkg/openlore/approval_fs.go)) sits
  *inside* the scope gate, so an out-of-scope write is denied before it could
  ever become an approval request. For a gated path it honors the caller's
  original precondition first (a stale write fails immediately — no doomed
  request is parked), captures the proposal-time base, records a **pending
  request**, fires `approval_pending`, and returns `vfs.PendingApprovalError`
  (which callers report informationally, not as a failure).

---

## 6. Capability gating (who can do what)

Commands are classified by `Action` in
[`pkg/shell/cmds/actions.go`](../pkg/shell/cmds/actions.go):

| Action | Verbs | Granted to |
|--------|-------|------------|
| `read` | `ls`, `cat`, `grep`, … (default) | everyone |
| `write` | `write`, `patch`, `tee`, `>`/`>>`, `sed -i` | recognized identities |
| `publish` | `publish` | recognized identities |
| `approve` | `approve`, `reject` | identities holding any approval capability |
| `spawn` | `spawn` | identities holding the explicit `spawn` capability |
| `admin` | server reconfiguration | reserved |

A session is given an allowed set (`shell.SetAllowedActions`). A command whose
action isn't allowed is treated as if it **doesn't exist** — an unauthorized
session can't even discover the write/publish/spawn surface. Anonymous /
unrecognized identities get the read-only set. `sed` is special-cased: only
`sed -i` is reclassified as a `write` (see `InvocationAction`).

---

## 7. Human-in-the-loop approvals (`/requests`)

A docset can declare `requires_approval` rules (path glob → required
capability). When a write hits a gated path, `approvalFS` records an
`ApprovalRequest` in a `RequestStore` (it stores the proposer, target, base hash,
proposed bytes, and required capability) instead of committing.

- **`/requests`** is a read-only computed FS (`NewRequestsFS`, mounted via
  `MountSystem`). `ls /requests` lists pending requests; `cat /requests/<id>`
  shows the request metadata and a diff of the proposed change.
- **`approve <id>` / `reject <id>`** ([`approve.go`](../pkg/shell/cmds/approve.go))
  are gated by `ActionApprove`. The approval backend re-checks that the approver
  holds the rule's required capability, then commits the stored bytes through the
  raw substrate via CAS against the captured base (so a change since the proposal
  is detected), or discards the request on reject.

---

## 8. Events & hooks

[`pkg/openlore/eventbus`](../pkg/openlore/eventbus/bus.go) is an in-process bus.
Event kinds: `on_startup`, `pre_read` (debounced per path), `post_write`,
`approval_pending`, `topic_refreshed`.

[`pkg/openlore/hooks`](../pkg/openlore/hooks/hooks.go) subscribes external
commands to these events (configured under `hooks:` in `openlore.yml`). A hook is
an external program invoked with an env-var protocol (`OPENLORE_EVENT`,
`OPENLORE_PATH`, `OPENLORE_BYTES`, `OPENLORE_AGENT`, …). This is how a write can,
e.g., trigger a notification or a downstream refresh without OpenLore knowing
anything about the target system.

---

## 9. Async external work: `spawn` + `/jobs`

The newest layer (Part D) lets a trusted identity kick off slow external work and
write its result back into the lore **for everyone**, without blocking the caller
and without bolting an auth server onto OpenLore.

- **`spawn --writes <path> [--append] -- <cmd…>`**
  ([`spawn.go`](../pkg/shell/cmds/spawn.go)) is gated by `ActionSpawn` (explicit
  `spawn` capability). At submit time it **fails fast** if `<path>` is outside the
  session's writable scope (via `WriteScopeFS.CanWrite`), then snapshots a
  *frozen* background context — the scoped `vfs.FileSystem`, the identity, and the
  resolved write-conflict policy — and returns a `job_<id>` + `/jobs/<id>` handle
  immediately.
- **`JobManager`** ([`jobs.go`](../pkg/openlore/jobs.go)) runs the command on a
  bounded worker pool (`max_jobs`) via the `hooks.Runner` (`sh -c`), then commits
  its stdout back through the **same** `cmds.WriteFile` seam on the frozen
  context — so CAS, per-docset policy, scoping, and the approval gate **all apply
  uniformly** to the background write. The captured scoped FS *is* the
  capability: no callback token, no external write endpoint, no durable queue.
- **`/jobs`** is a read-only computed FS (`NewJobsFS`, mounted via `MountSystem`):
  `ls /jobs` lists jobs; `cat /jobs/<id>` shows `running` / `done` / `failed`,
  target, command, timestamps, and the terminal detail (bytes written, pending
  request id, or error).

**The trade we accept:** jobs are in-memory, so a server restart loses in-flight
work. Shutdown drains for a few seconds first to shrink the loss window. Anything
needing durability stays an ordinary gated write, not a job.

---

## 10. Configuration summary

In `openlore.yml` (global) and per docset in `lore.json`:

| Setting | Scope | Meaning |
|---------|-------|---------|
| `readonly` | global / per-docset | Global is a hard physical lock (default `true`). A per-docset `readonly: false` cannot loosen a global lock; a per-docset `readonly: true` excludes that docset from writes. |
| `write_conflict_policy` | global / per-docset | `hash` (CAS, default) or `last_write_wins`. Per-docset overrides global. |
| `requires_approval` | per-docset | List of `{ path, capability }` rules that gate matching writes behind `approve`. |
| `max_jobs` | global | Max concurrent async `spawn` jobs (default `8`). |
| `hooks` | global | External commands subscribed to `on_startup`/`pre_read`/`post_write`/`approval_pending`. |

The substrate is read-only unless `readonly: false` is set globally (or
`--readonly=false` on the CLI). To enable per-agent writes, give each identity
`publish` docsets — those become its writable scope.

---

## 11. Source map

| Concern | File |
|---------|------|
| FS contract, `WriteOpts`, errors, policy | [`pkg/vfs/vfs.go`](../pkg/vfs/vfs.go) |
| On-disk atomic substrate, `MergeFS` | [`pkg/openlore/vfs.go`](../pkg/openlore/vfs.go) |
| The write seam (CAS / append / policy) | [`pkg/shell/cmds/write.go`](../pkg/shell/cmds/write.go) |
| Per-identity scope gate | [`pkg/openlore/scoped_write_fs.go`](../pkg/openlore/scoped_write_fs.go) |
| Approval gate | [`pkg/openlore/approval_fs.go`](../pkg/openlore/approval_fs.go) |
| Requests store + `/requests` + approve/reject | [`pkg/openlore/approval.go`](../pkg/openlore/approval.go), [`pkg/shell/cmds/approve.go`](../pkg/shell/cmds/approve.go) |
| Capability classes | [`pkg/shell/cmds/actions.go`](../pkg/shell/cmds/actions.go) |
| Events / hooks | [`pkg/openlore/eventbus/bus.go`](../pkg/openlore/eventbus/bus.go), [`pkg/openlore/hooks/hooks.go`](../pkg/openlore/hooks/hooks.go) |
| Async jobs (`spawn`) + `/jobs` | [`pkg/shell/cmds/spawn.go`](../pkg/shell/cmds/spawn.go), [`pkg/openlore/jobs.go`](../pkg/openlore/jobs.go) |
| Per-session FS composition & gating | [`pkg/openlore/server.go`](../pkg/openlore/server.go) |
| Config fields | [`internal/config/config.go`](../internal/config/config.go) |
