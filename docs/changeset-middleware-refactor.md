# Refactor Plan: ChangeSet + Middleware Write System

Status: **locked** (design agreed). Phases A–E implemented (SSH end-to-end
proven; MCP unification deferred — see E3 note).

Replaces the baked-in approval control plane with a minimal, generic mechanism:
an ordered write log with compare-and-swap (CAS) at a serialized applier, a
composable middleware chain on both the write and read paths, and a durable
`ChangeSet` primitive. **All approval/policy concerns move out of go-openlore
core into the knowledge-backend (KB).**

This spans two repos:

- `go-openlore` — the storage mechanism (this repo).
- `knowledge-backend` — the policy consumer (`../knowledge-backend`, wired via
  the `replace` directive in its `go.mod`).

---

## 1. Why

Today go-openlore bakes an entire approval control plane into the storage core:
`RequestStore` (persistence), `RequestsFS` (`/requests` render), `approvalFS`
(write interception + docset-rule gating), `approvalBackend` (approve/reject +
capability checks + CAS replay), `approve`/`reject` shell commands, and a
`KindApprovalPending` event. None of it is wired in the KB deployment — the KB
constructs the server with no `DataDir` and no docsets, so `approvalFS`/
`RequestStore` never activate, and the KB enforces approvals entirely in its own
DB layer (proposals → `ClassRequiresApproval` → `materializeProposal`).

So the core carries dead, single-purpose primitives instead of a general seam a
consumer can implement. This refactor collapses five overlapping concepts
(approval FS, request store, request FS, approve/reject backend, event bus) into
**one interception concept (middleware)** plus **one durable primitive
(`ChangeSet`)**, and pushes every policy decision to the KB.

---

## 2. Target architecture

```
WRITE PATH
 caller ─▶ [scope: fixed, deployment-owned]           ← auth/write isolation, non-bypassable
        ─▶ [admission middleware chain (goroutine)]    ← approval (KB), pre-commit shellexec (can block)
             │ admit                 │ gate
             ▼                       ▼
        append to ─────┐        persist held ChangeSet (KB file store)
        ORDERED LOG    │             │ …human approve…
             ▲         │             ▼
             └─────────┼──── CommitChangeSet == append to LOG (skips admission; already admitted)
                       │
                       ▼
        SERIALIZED APPLIER (sole input = the LOG): CAS vs current
                       │
                       ├──▶ substrate
                       ├──▶ [post-commit hooks: sync] (fail_on_error=true, timeout, log keeps moving)
                       └──▶ emit events → tailable filtered streams (KB routes; ephemeral; tail -f / SSE)

READ PATH
 caller ─▶ [read middleware chain]  ← pre_read shellexec (git pull, debounced, can block) ─▶ substrate
```

### 2.1 Ownership map (authoritative)

Every component, where it lives today, and where it ends up. Four fates:
**KEEP** (stays in go-openlore, unchanged), **NEW** (new go-openlore mechanism),
**MOVE→KB** (capability rebuilt as a knowledge-backend OSS-staging package under
`internal/openlore/…`, the same pattern as `scorer`/`notifier`/`policy`), and
**DELETE** (removed; capability dropped or reborn differently).

| Component | Today (go-openlore) | Fate | Ends up in |
|---|---|---|---|
| CAS primitives: `WriteOpts`, `RemoveOpts`, `TreeSnapshot`/`TreeOp`, `PreconditionError`, `TreeStaleError`, `WritableFS` | `pkg/vfs/vfs.go` | **KEEP** | go-openlore `pkg/vfs` |
| Session-shell install seam: `buildSessionShell` / `shellForContext` | `pkg/openlore/server.go` | **KEEP** (extended with chains) | go-openlore `pkg/openlore` |
| Auth-mode write scope: `scopedWriteFS` | `pkg/openlore/scoped_write_fs.go` | **KEEP** (fixed pre-chain layer; unused by KB) | go-openlore `pkg/openlore` |
| `ChangeSet` / `WriteChange` / `DeleteChange` | — (coupled `ApprovalRequest`) | **NEW** | go-openlore `pkg/vfs` |
| `CommitChangeSet` (CAS replay = append to log) | logic buried in `approvalBackend` | **NEW** (extracted) | go-openlore `pkg/vfs` |
| `PendingChangeError` | `PendingApprovalError` in `pkg/vfs` | **NEW** (slimmed rename) | go-openlore `pkg/vfs` |
| Ordered write log + serialized applier (CAS site) | — | **NEW** | go-openlore `pkg/openlore` |
| Middleware chains (admission/post-commit/read) + `WriteMiddlewareProvider` + `Actor{ID}` | — | **NEW** | go-openlore `pkg/openlore` |
| Tailable-stream mechanism (`tail -f`/SSE) + `Emit` interface | `Feed` fan-out via bus | **NEW** (mechanism kept, re-shaped) | go-openlore `pkg/openlore` |
| `shellexec` default plugin (config-driven external commands) | shell `hooks` package | **NEW** (reborn as plugin) | go-openlore (default plugin) |
| Held-changeset **store** (file-backed proposed bytes) | `RequestStore` in `approval.go` | **MOVE→KB** | KB `internal/openlore/approvals/` |
| Gating **decision** (is this write approvable?) | `requiresApproval` + docset rules | **MOVE→KB** | KB (uses existing `ClassRequiresApproval`) |
| Approve/reject **resolution** (minus CAS) + status/capabilities/approver/proposer | `approvalBackend` + `ApprovalRequest` fields | **MOVE→KB** | KB `internal/openlore/approvals/` + HTTP endpoint |
| Approval **middleware** (fail-closed gate, path→partition, park+`PendingChangeError`) | `approvalFS` | **MOVE→KB** (as middleware) | KB `internal/openlore/approvals/` |
| Event **writers** + stream routing/filtering | DBFS auto-publish + scattered KB publishes | **MOVE→KB** | KB (drives `Emit`) |
| `kb publish --wait` coordination | bus `KindTopicRefreshed` + worker | **MOVE→KB** | KB `internal/openlore/publish` + worker |
| `eventbus` fan-out | `pkg/openlore/eventbus` | **DELETE** | — (feed re-shaped as tailable streams) |
| Shell `hooks` package | `pkg/openlore/hooks` | **DELETE** | — (capability → `shellexec` plugin) |
| `RequestsFS` + `/requests` mount | `approval.go` | **DELETE** | — |
| `approve`/`reject` shell cmds + `OPENLORE_CAPABILITIES` | `pkg/shell/cmds/approve.go` | **DELETE** | — (resolution is KB HTTP) |
| Docset `RequiresApproval` / `ApprovalRule` config | `internal/config/config.go` | **DELETE** | — (gating is KB DB) |
| `KindApprovalPending` | `pkg/openlore/eventbus/bus.go` | **DELETE** | — |

**One-line summary:** go-openlore keeps *storage mechanism* (CAS, ordered log,
middleware seams, tailable streams, `ChangeSet`); everything *policy* — the
approver concept, gating decision, held-changeset store, resolution, and event
routing — moves up into the knowledge-backend OSS packages; the approval CLI,
event bus, shell-hooks package, and docset approval config are deleted outright.

### go-openlore keeps (mechanism only)

- **`vfs.ChangeSet`** — immutable, serializable description of one atomic
  mutation: `{Target, Action(Write|Delete), Write{Bytes, Opts vfs.WriteOpts},
  Delete{Opts vfs.RemoveOpts}}`. The payload carries the caller's own
  `WriteOpts`/`RemoveOpts` verbatim, so it faithfully represents every write
  mode — unconditional (last-write-wins), `IfMatch` CAS, `IfNoneMatch`
  create-only, and snapshot-guarded vs unconditional delete — through both the
  log and an approval hold. No approver / status / proposer / capability fields.
- **`vfs.CommitChangeSet(fs, cs)`** — replay a held changeset by appending it to
  the ordered log (CAS applied at the applier). CAS drift →
  `PreconditionError` / `TreeStaleError`.
- **`vfs.PendingChangeError{ChangeSet, Ref}`** — sentinel for a write handed off
  to a middleware (lives in `vfs` to avoid the `cmds → vfs → openlore` cycle).
- Existing CAS primitives (`WriteOpts`, `RemoveOpts`, `TreeSnapshot`,
  `PreconditionError`, `TreeStaleError`, `WritableFS`) — unchanged.
- **Ordered write log + serialized applier** — the single write serializer; CAS
  and post-commit hooks run here, uniformly for fresh writes and approval
  replays.
- **Middleware seams** in `buildSessionShell` (so SSH + MCP + HTTP are covered
  by one factory): admission (pre-commit), post-commit, and read chains.
- **`WriteMiddlewareProvider`** (and read equivalent) plugin hook; core composes
  provider middleware in registration order, **after** the fixed scope layer.
- **Tailable-stream mechanism + `Emit` interface** — named, appendable,
  `tail -f`/SSE-able virtual files. Content-agnostic. Ephemeral (in-memory).
- **`shellexec` default plugin** — reproduces the old `openlore.yml` hooks
  (`pre_read`, `post_write`, plus a new pre-commit slot) as middleware.

### knowledge-backend owns (all policy)

- **Approval middleware** — synchronous, **fail-closed** DB gating
  (`ClassRequiresApproval` + core-partition rules), owns VFS-path → partition/
  class mapping; on gated paths persists the `ChangeSet` and returns
  `PendingChangeError`.
- **Held-changeset store** — the relocated file-backed `RequestStore` (proposed
  bytes stay immutable on disk for exact CAS replay + delete review), rooted
  under the KB DB directory (per-tenant if multi-tenancy on). Separate from the
  proposals table.
- **Resolution** — human-triggered via a KB HTTP endpoint → `CommitChangeSet`
  (append to log). Idempotent + drift-safe (stale on `Precondition`/`TreeStale`).
- **Event writers + stream routing/filtering** — the KB emits events into named
  streams (`/feed`, `/partitions/{p}/feed`, …) via the `Emit` interface; all
  subset/filter logic is KB-side.
- **`Actor{ID}`** — passed into admission middleware from the session
  `Identity`; never enters the durable `ChangeSet`.

### Deleted from go-openlore

`eventbus`-as-fanout, shell `hooks` package (→ `shellexec` plugin), `RequestsFS`,
`approve`/`reject` cmds + `OPENLORE_CAPABILITIES`, docset `RequiresApproval` /
`ApprovalRule`, `approvalFS`, `KindApprovalPending`, and the `approval.go`
request/backend types.

---

## 3. Locked decisions

1. Middleware chain installed in `buildSessionShell` (covers SSH/MCP/HTTP via
   `shellForContext`).
2. Approve/reject CLI + `/requests` mount **deleted**; resolution is 100% KB
   (HTTP + proposals UI).
3. KB DB is the **sole** gating authority; docset `RequiresApproval` deleted;
   middleware does a synchronous fail-closed DB lookup and owns path→partition
   mapping.
4. Held changesets live in a **separate dedicated store**, not folded into the
   proposals table.
5. Store = **relocated file-backed** `RequestStore` in the KB (immutable on-disk
   proposed bytes).
6. Async write model: writes flow through admission middleware into an **ordered
   log**; a **serialized applier** does CAS against current state and commits.
   `CommitChangeSet` == append to the log. There is no separate "ungated writer"
   — the applier is the sole writer.
7. Scope (auth/write isolation) is a **fixed, non-bypassable layer before** the
   plugin middleware chain — not itself middleware. In the KB it is the
   `WithAgent` DBFS injected via `sessionFSFn`.
8. `Actor{ID}` flows into admission middleware from session identity; never into
   `ChangeSet`.
9. Eventbus-as-fanout **deleted**; the tailable feed **stays** as a filtered,
   ephemeral projection (distinct from the log). Writers move to the KB behind an
   `Emit` interface; filtering is entirely KB-side.
10. Shell hooks survive as an opt-in **`shellexec` plugin**; read + write command
    execution both maintained. Read-middleware seam **added** (symmetric).
11. Hook execution: **sync by default** on both seams (`async` opt-in);
    `fail_on_error` defaults **true**; timeout defaults **on** (30s, timeout =
    failure). Pre-read / pre-commit failures **abort** the operation;
    post-commit failures are **surfaced but the log keeps moving** (write already
    durable; external side-effects may drift — reconciliation is operator's job).
12. Conflict policy is **per-write** (a field the write carries), not per-file.
    The applier dispatches per entry; supporting CAS + DMP + last-write-wins
    needs no architectural change (reuses `WriteConflictPolicy`). No per-file
    policy registry.
13. **CAS feedback timing = await.** `Submit` blocks until the serialized applier
    commits the entry and returns the committed hash or CAS error (per-entry
    buffered reply channel). Preserves the "re-read and retry" contract.
14. **Log durability = none (in-memory).** Because await means nothing is
    "accepted" until applied+durable in the substrate, the log is a plain
    in-memory ordered channel. Durability = substrate (committed) + KB
    held-changeset store (pending/approved). No WAL, no replay/offset
    bookkeeping. A crash loses only in-flight, un-acknowledged writes → caller
    retries.
15. **Concurrency = one applier goroutine + per-entry reply channel.** Callers
    (already their own request goroutines) run admission inline, submit, and
    block on the reply. The applier is the sole substrate writer. **Graceful
    shutdown:** `writeLog.Close(ctx)` (called from `Server.Shutdown`, itself
    driven by the entrypoint's signal handler) stops new submits (→
    `ErrLogClosed`), drains queued + in-flight entries so every acknowledged
    write completes, and waits for the applier to exit.

### Explicitly deferred (revisit later)

- Whether strict **submission ordering** is guaranteed under concurrent
  submitters (currently: FIFO into the channel; apply-time CAS resolves
  conflicts, commit order = channel arrival order).
- Durable/replayable event streams (currently ephemeral in-memory).
- DMP / alternate write semantics (architecture supports it per-write; not built).

---

## 4. Implementation phases

### Phase A — go-openlore primitives (no behavior change to callers yet) — DONE

A1. ✅ Added `vfs.ChangeSet`, `WriteChange`, `DeleteChange`, `PendingChangeError`
    (`pkg/vfs/changeset.go`).
A2. ✅ Extracted CAS-replay into `vfs.CommitChangeSet(fs, cs)` (write via
    IfMatch/IfNoneMatch, delete via snapshot `Expected`). Tested incl. drift.
A3. ✅ Ordered write log + single applier goroutine (`pkg/openlore/writelog.go`):
    await via per-entry reply channel, in-memory (no WAL), graceful
    `Close(ctx)` draining in-flight + queued. Tests pass under `-race`
    (order/await, error propagation, submit-after-close, drain-on-shutdown).

    NOTE: `CommitChangeSet` in `writelog` currently commits directly; post-commit
    hooks + feed emit are added at the applier in Phase C. The old
    `approvalFS`/`RequestStore` still stand (deleted in Phase C).

### Phase B — go-openlore middleware seams

B1. Define `WriteMiddleware`/`WriteHandler`, admission + post-commit + read
    chains, and `WriteMiddlewareProvider` (+ read provider). `Actor{ID}` type. — DONE
    (`pkg/openlore/middleware.go`, `pkg/openlore/middleware_test.go`.)
B2. Wire the chains into `buildSessionShell`: fixed scope layer (existing) →
    `sessionFSFn` → admission chain → log; post-commit chain at the applier;
    read chain around `Stat`/`ReadDir`/`ReadFile`. Verify MCP/HTTP via
    `shellForContext`.
    - **Write path DONE.** `middlewareFS` (`pkg/openlore/middleware_fs.go`) is
      the innermost writable wrapper: it turns every write/mkdir/remove into a
      `ChangeSet`, runs `Server.writeChain()` (admission → terminal submit), and
      awaits the global `writeLog`. `writeLog` created in `NewServer` over
      `s.merge` (readonly=false only), closed in `Shutdown`. Covered by SSH +
      MCP + HTTP because it lives in `buildSessionShell`.
    - **Decision 1 (global applier):** one `writeLog` over the single substrate;
      attribution is the `Actor{ID}` carried on each log entry to the
      post-commit chain, NOT a per-session substrate. (KB's per-agent `DBFS`
      attribution migrates to `Actor` in Phase D.)
    - **Decision 2 (mkdir/remove are log ops):** `ChangeSet` now has
      `write`/`mkdir`/`mkdir_all`/`remove`/`remove_all` actions so directory
      creation and removal serialize through the log too — a write can never
      race ahead of, or land on, a removed path.
    - **Post-commit chain DONE at the applier** (`Server.postCommitChain()` →
      `writeLog.postCommit`): runs after a durable commit with
      `CommitInfo{ChangeSet, Hash, Actor}`; failures logged, log keeps moving
      (decision #11).
    - **Read chain DONE.** `readChainFS` (`pkg/openlore/read_chain_fs.go`) runs
      `Server.readChain()` in front of `Stat`/`ReadDir`/`ReadFile`; a middleware
      may abort a read by returning an error. Installed in `buildSessionShell`
      as the innermost read wrapper, only when `s.readMW` is non-empty (zero
      overhead otherwise).
    - All three chains compose uniformly from provider slices (`s.writeMW`,
      `s.readMW`, `s.postCommitMW`), currently empty in core.
    - **Provider registration DONE** (built with C4): `Server.registerPlugin(p any)`
      type-asserts the three provider interfaces and appends into `s.writeMW` /
      `s.readMW` / `s.postCommitMW`. Called in `NewServer` before the write log is
      built (post-commit chain is composed at `newWriteLog`). External-plugin
      registration timing (KB) is revisited in Phase D.
B3. Update `pkg/shell/cmds` `write`/`rm` to treat `PendingChangeError` as
    exit-0 and print `Ref`. — DONE
    - `write`/`tee`, `rm`, `publish`, `patch` now handle `*vfs.PendingChangeError`
      (exit 0, prints "<cmd>: <target> change pending as <Ref>") alongside the
      legacy `*vfs.PendingApprovalError`. Shared `pendingChangeLine` helper in
      `write.go`. Tests: `pkg/shell/cmds/pending_change_test.go`.

### Phase C — go-openlore deletions + shellexec plugin

C1. Delete `approval.go` (RequestStore/RequestsFS/approvalBackend/
    ApprovalRequest), approval-specific `approval_fs.go`, `approve.go` cmds +
    `OPENLORE_CAPABILITIES`, docset `RequiresApproval`/`ApprovalRule`,
    `KindApprovalPending`. — DONE
    - Deleted `pkg/openlore/approval.go`, `approval_fs.go`, and their tests
      (`approval_test.go`, `approval_fs_test.go`, `approval_delete_test.go`,
      `approval_events_test.go`, `approve_flow_test.go`); deleted
      `pkg/shell/cmds/approve.go`.
    - `server.go`: removed `requests *RequestStore` field, the `/requests`
      control-plane mount + `cmds.Approvals` wiring, the `approvalFS` layer in
      `buildSessionShell`, the `ActionApprove` grant, and the
      `OPENLORE_CAPABILITIES` env var. Preserved `hasCapability` (moved here;
      still used for `spawn` gating).
    - `cmds`: removed `ActionApprove` + `approve`/`reject` classifications and
      registrations, the APPROVALS help block, and `DocsetInfo.Approval` (+
      `lore` table attribute). Removed `vfs.PendingApprovalError` handling from
      `write.go`/`rm.go`/`publish.go`/`patch.go` and `jobs.go`.
    - `internal/config`: removed docset `RequiresApproval`/`ApprovalRule`.
    - `pkg/vfs`: removed `PendingApprovalError`.
    - `eventbus`: removed `KindApprovalPending`; `hooks`: removed
      `HookSet.ApprovalPending` field + wiring.
    - go-openlore `go build ./... && go test ./...` green (incl. `-race`); KB
      still compiles against updated go-openlore (local replace).
C2. Delete the `eventbus` fan-out and the shell `hooks` package. Move
    `kb publish --wait` coordination fully into the KB. — DONE
    - Deleted `pkg/openlore/hooks/` (relocated `Runner`/`ShellRunner` into
      `pkg/openlore/runner.go`); `shellexec.go`/`jobs.go`/`server.go` use
      `Runner`/`ShellRunner`; removed `Hooks` config.
    - Detached core from the bus: `vfs.go`/`server.go`/`jobs.go` no longer hold
      a bus, `WithBus`, or publish post_write/post_delete.
    - Deleted the `pkg/openlore/eventbus` fan-out package entirely (no
      remaining references in either repo).
    - `kb publish --wait`: no separate wait/coordination primitive exists; per
      the design, any publish waiter is a KB-only concern and is NOT built on
      the stream. Nothing to move.
C3. Build the tailable-stream mechanism + `Emit` interface (read side:
    `tail -f`/SSE; append side: interface). — DONE
    - `pkg/openlore/events.go`: `Event`, `EventKind` (+ `KindOnStartup`,
      `KindPreRead`, `KindPostWrite`, `KindPostDelete`, `KindTopicRefreshed`),
      `Emit`/`EmitFunc`, `EventFilter`, `MatchAll`/`MatchPartition`.
    - `pkg/openlore/stream.go`: in-memory ring + live subscribe + `OpenReader`
      (`tail -f`) + SSE `HTTPHandler` + `EncodeEvent`. Tested (`stream_test.go`).
C4. Build the `shellexec` default plugin: config-driven (`pre_read`,
    `post_write`, `pre_commit`), sync-default, `fail_on_error=true` default,
    default timeout, same `OPENLORE_*` env protocol. Post-commit failures log +
    continue; pre-* failures abort. — DONE
    - `pkg/openlore/shellexec.go` — `shellexecPlugin` implements all three
      providers. pre_read → read mw (per-path debounce, 2s default; abort on
      fail), pre_commit → write mw (reject on fail), post_write → post-commit
      (fire-and-forget, never halts log). Defaults: sync, `fail_on_error=true`,
      30s timeout. Env: `OPENLORE_{DATA_DIR,EVENT,PATH,AGENT,ACTION,BYTES,HASH,
      READ_KIND,EXTRA_*}`. `async: true` opt-in (background, cannot abort).
    - Config: `config.ShellexecConfig`/`ShellexecCmd` (`shellexec:` block,
      string durations, `fail_on_error *bool` → default true). Wired in
      `NewServer` via `registerPlugin` before the write log is built.
    - Reuses `hooks.Runner`/`ShellRunner` for now; the runner moves out of the
      doomed `hooks` package in C2.
    - Tests: `pkg/openlore/shellexec_test.go` (env, allow/reject/abort,
      debounce, post-commit never-halts, async no-abort, config
      defaults/validation).
C5. `go build ./... && go test ./...` green in go-openlore. — DONE
    (verified incl. `-race` on `pkg/openlore` + `pkg/vfs` after the eventbus/
    hooks deletions).

### Phase D — knowledge-backend plugin

D1. New `internal/openlore/approvals/` package (OSS-staging, mirrors
    `scorer`/`notifier`/`policy`): approval admission middleware
    (fail-closed DB gating, path→partition mapping), relocated file-backed held
    store, resolver (`CommitChangeSet`), `Actor` from identity. — DONE
    - `store.go` (file-backed held-change store), `policy.go`
      (`GovernancePolicy`: DB-backed, fail-closed, gates the
      `governance.config_approval` list of `/config/*.yml` basenames),
      `middleware.go` (`WriteMiddlewareProvider`; parks gated writes as
      `*vfs.PendingChangeError`, fail-closed on policy error), `resolver.go`
      (approve → `CommitChangeSet`; CAS drift → STALE; idempotent; a per-resolver
      mutex serializes the read-commit-update sequence).
D2. HTTP endpoint(s) for human resolution (`approve`/`reject` a held changeset).
    Consider unioning with the proposals UI (read-side only). — DONE
    - `approvals/http.go`: `GET /approvals`, `GET /approvals/{id}`,
      `POST /approvals/{id}/approve`, `POST /approvals/{id}/reject`. STALE → 409,
      missing → 404. Mounted from `cmd/server/main.go` via `Server.Approvals()` /
      `Server.Resolver()`.
D3. KB event writers: emit into named streams via the `Emit` interface; move all
    routing/filtering here. Delete the KB `eventbus` staging shim; re-point the
    feed to the new mechanism. — PARTIALLY DONE
    - DONE: feed re-pointed to the go-openlore `Stream` (`api.Feed` wraps
      `Stream`, implements `Emit`); all KB writers (DBFS post_write, `publish`,
      worker `topic_refreshed`, teach, gdrive) call `Emit` directly; notifier +
      worker-queue converted from bus `Subscriber`s to `Emit` sinks/adapters;
      `main.go` wires the feed as the single `Emit` sink (no bus).
    - REMAINING: the KB `internal/openlore/eventbus` package is still an alias
      shim over upstream `openlore` (kept to avoid the package-name collision);
      decide whether to keep it or import `openlore` directly. Named/multiple
      streams + KB-side routing/filtering not yet built (single stream today).
D4. Wire it in `internal/openlore/server.go`: register the approvals middleware +
    shellexec (if used) as providers; keep `WithAgent` scope via `sessionFSFn`.
    — DONE
    - KB `NewServer` now builds the DBFS first and installs it via
      `openlore.NewServerWithRootFS(dbFS, WithReadonly(cfg.Readonly), …)` so the
      ordered write log + admission chain exist over the DBFS at construction
      (a late `SetRootBashFS` would leave the log with no writable backend).
    - The approvals middleware is registered via `srv.RegisterPlugin(mw)` before
      serving; the resolver commits approved changes via `srv.CommitChangeSet`.
    - `sessionFSFn` now WRAPS `base` (the go-openlore `middlewareFS`) instead of
      replacing it: `session_fs.go` serves agent-scoped reads from
      `dbFS.WithAgent(id.User)` while routing all writes through `base` (→ log +
      admission), confined to the supported `/config/*.yml` write surface
      (scope-before-policy). The KB `write` command was fixed to use `ctx.FS()`
      (not raw DBFS) and to treat `*vfs.PendingChangeError` as a pending success.

### Phase E — verification

E1. go-openlore unit tests: chain admit/gate, delete gate, CAS drift,
    `RemoveOpts.Expected`, ordering, post-commit hook sync + failure semantics,
    read hook debounce. — DONE (incl. `pkg/openlore/server_seams_test.go`:
    `NewServerWithRootFS` live write log, late `RegisterPlugin`, late post-commit,
    `CommitChangeSet` bypasses admission but still commits).
E2. KB tests: approvals middleware (gated vs ungated path, fail-closed on DB
    error), resolver idempotency + drift → stale, path→partition mapping. — DONE
    (`approvals/approvals_test.go`, `approvals/http_test.go`).
E3. **End-to-end gap closed (SSH):** `internal/openlore/approvals_e2e_test.go`
    starts the real KB SSH server, and over a live SSH session a governance-gated
    `write /config/agents.yml` defers (reports pending), appears in the held
    store, and only commits after `Resolver.Approve` (the same resolver the HTTP
    approve endpoint drives); an ungated `/config/openlore.yml` write commits
    inline. **MCP still bypasses** the shared write log (KB's `/mcp` handler is a
    separate custom stack); unifying it onto `buildSessionShell` is a larger,
    separate effort, deliberately deferred.
E4. `go test ./...` green in both repos; run the KB e2e suite
    (`eval/run_eval.sh`), regenerating the base DB if gating changes stored data
    (`FORCE_REGEN_DB=1`). — go-openlore green; KB `internal/openlore/...` green.
    (Pre-existing unrelated failures remain in KB `internal/handlers` — nil
    `gitrepo.Manager` test setup — not touched by this refactor. `eval/` suite
    not run here.)

---

## 5. Key risks / invariants to hold

- **Cycle:** `PendingChangeError` + `ChangeSet` + `CommitChangeSet` must live in
  `pkg/vfs` (`cmds` imports `vfs`; `openlore` imports `cmds`).
- **Scope before policy:** an out-of-scope write must be denied before any
  plugin middleware sees it. Scope is fixed-first, non-bypassable.
- **Immutable ChangeSet:** middleware inspect/decide only; never rewrite bytes/
  snapshot. Guarantees the parked changeset and the eventual commit are
  byte-identical (sound CAS).
- **The applier's sole input is the log.** Both fresh admitted writes and
  approved held changesets append to the **log**, never directly to the applier —
  a second producer would reintroduce concurrency and break race-free CAS +
  ordering. `CommitChangeSet` == append to the log; there is no separate commit
  path.
- **Approvals skip admission, not the log.** An approved held ChangeSet was
  already admitted (it passed scope and reached the approval middleware), so it
  appends **below** the admission chain. Re-running admission would let the
  approval middleware re-defer it into an infinite loop.
- **CAS at the applier is authoritative** (serialized, race-free). Resolvers
  treat "already committed / stale" as terminal, not error.
- **Fail-closed gating:** a DB error in the approval middleware must reject the
  write, never silently commit.
- **Applier liveness:** sync post-commit hooks run inside the serialized applier;
  the mandatory timeout is what prevents a hung command from wedging all writes.
```
