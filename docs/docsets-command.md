# Plan: `lore docsets` command

## Goal

Give an agent a way, from inside its SSH/MCP shell session, to ask **which docsets it
can access, where they are mounted in the virtual filesystem, and their attributes**.
Today an agent can learn its identity (`whoami`) and its writable *publish inboxes*
(`publish` with no args), but there is no single command that answers "what can I read
and write, and at what paths?".

## Command shape: `lore` dispatcher, `lore docsets` subcommand

The shell surface (`pkg/shell/cmds`) is flat POSIX-style with no subcommand groups. We
introduce `lore` as a **product-named dispatcher** so future introspection can hang off
it (`lore whoami`, etc.) without reshaping a command's contract later.

- **Bare `lore`** prints subcommand help and exits **0**:
  ```
  Usage: lore <command>

  Commands:
    docsets   List the docsets you can access, their paths, and attributes

  Run 'lore <command>' for a specific view.
  ```
- **`lore <unknown>`** writes an error + usage to stderr and exits **1**.
- **`lore docsets`** is the first (and currently only) subcommand.

## `lore docsets` output

Space-aligned table with one row per mount, named attribute tokens
(grep-friendly), and `-` for empty:

```
DOCSET    GRANT  ATTRIBUTES  PATH             TARGET
public    ro     -           /docs/public     -
backend   rw     -           /docs/backend    -
home      rw     home,inbox  /home/backend    -
home      rw     alias       /backend         /home/backend
```

- **`GRANT`** — the named grant held on the docset (`ro`, `rw`, `publish`, or a
  plugin-contributed grant).
- **`ATTRIBUTES`** — named tokens, any of:
  - `home` — this docset is the session's home docset (`$HOME`).
  - `inbox` — the docset declares an inbox.
  - `alias` — this row is an alternate mount whose canonical path is `TARGET`.
- **`PATH`** — the display (virtual) path for this mount. Never an on-disk source path.
- **`TARGET`** — the canonical display path for aliases; `-` for canonical rows.
- **Order** — docset name, with canonical rows before aliases of that docset.
- **Header** — kept; agents can `tail -n +2` / `grep -v '^DOCSET'` for machine parsing.

## Two concerns, kept separate

Docset **direct writability** and the **publish inbox** are distinct:

- **Direct writability** = the FS-authoritative set (`writableDocsetRoots`: in the
  session's write scope − per-docset `readonly` − global lock). If a docset is writable
  to you, you write to it directly with the normal write verbs. This is what `ACCESS`
  (`r`/`rw`) reflects. `publish_dir` has nothing to do with it.
- **Publish inbox** (`publish_dir`) = a separate mechanism to submit content into an
  inbox for later curation/addition — notably useful for **readonly** docsets. It stays
  its own concern; `lore docsets` surfaces only its *presence* via the `publish`
  attribute token, never the inbox path or size. The `publish` command owns the inbox
  details.

## Plumbing: kill `OPENLORE_DOCSETS`, move to per-session in-memory state

Today the session's writable info lives in two stringly-typed places: the
`OPENLORE_DOCSETS` env var and the global `PublishTargets`/`RegisterPublishTarget`
registry. Both are deleted and replaced by per-session in-memory state, computed once
server-side (where the authority already lives) and exposed via `CmdContext`.

Two separate accessors on `CmdContext` (matching the two concerns):

```go
// pkg/shell/cmds — next to CmdContext
type DocsetInfo struct {
    Name       string
    Paths      []string // display paths
    Writable   bool     // direct FS writability → r vs rw
    Home       bool     // attribute
    HasPublish bool     // attribute (has a publish inbox)
    Approval   bool     // attribute (requires_approval applies)
}

type PublishTarget struct {
    Name        string
    MaxFileSize int64
}

// CmdContext gains:
Docsets() []DocsetInfo
PublishTargets() []PublishTarget
```

- `lore docsets` reads `ctx.Docsets()`.
- `publish` reads `ctx.PublishTargets()` (replacing the `OPENLORE_DOCSETS ∩ PublishTargets`
  lookup). `RegisterPublishTarget` / package-level `PublishTargets` / `OPENLORE_DOCSETS`
  are removed entirely.
- The `Shell` stores both slices; the server populates them from the resolved identity +
  auth config at session creation.

### Deleted

- `OPENLORE_DOCSETS` env var (set in `server.go`, read in `publish.go`).
- `cmds.PublishTargets`, `cmds.RegisterPublishTarget`, `cmds.findPublishTarget`
  (global registry) — moved to per-session state.

## Unenforced mode → synthetic `public` docset

`s.auth` is always non-nil now; `authEnforced` is the real signal. Rather than special-
case `!authEnforced` in the command, we make unenforced mode a normal one-docset policy
so every consumer reuses the standard docset path.

In `NewServer`, when `!authEnforced` (no auth file), populate the empty policy:

```go
paths := []config.PathMapping{{Source: "/", Display: "/"}}
s.auth.Docsets = map[string]config.DocsetSpec{"public": {Paths: paths}}
s.auth.Lore    = map[string][]string{"default": {"public"}}
```

Then simplify `resolveIdentity`'s unenforced `else` branch to:

```go
id.LoreName = "default"
id.PathAccess = s.resolveLorePathAccess("default")
id.Scopes = []string{ScopeFull}
```

deleting the old `merge.mounts` loop.

- `public` `Home` is always false (unenforced sessions have empty `HomeDir`).
- `public` has no `publish_dir` → no `publish` attribute.
- Writes are unrestricted in unenforced mode (`scopedWriteFS` only wraps when
  `authEnforced`), so the `DocsetInfo` builder marks `Writable` per mode:
  - unenforced → `!globalReadonly`
  - enforced → membership in `writableDocsetRoots`

So `lore docsets` in unenforced mode shows e.g. `public  rw  -  /` (rw when the global
lock is open), with no special branch in the command.

## Home docset name

Add `HomeDocset string` to `Identity`, populated during identity resolution (the auth
config already names it via `AuthIdentity.Home`). The `DocsetInfo` builder sets
`Home = (docset.Name == id.HomeDocset)`. (Chosen over path-matching `HomeDir`, which is
fragile when docsets share a first display path.)

## Tests

- `lore_test.go`: bare `lore` help + exit 0; unknown subcommand → stderr + exit 1;
  `lore docsets` table for r-only / rw / mixed; attribute tokens (`home`, `publish`,
  `approval`); `-` for none; path ordering; header present.
- Builder/server test: `Docsets()` and `PublishTargets()` populated correctly per
  identity (enforced) and for synthetic `public` (unenforced); folder mounts folded in.
- Regression: `publish` still lists the same inboxes via `PublishTargets()` after the
  env/registry removal.

## Docs

- README "Supported Commands": add a `lore` / `lore docsets` introspection entry.
- Note the `publish` (inbox) vs `lore docsets` (direct access) distinction.

## Non-goals

- No CLI subcommand (`openlore docsets`) — access is per-session.
- No change to `publish` behavior/output beyond its data source.
- No on-disk source path disclosure — display paths only.
- No JSON/format flags in v1 (output is aligned + comma-joined, greppable).

## Decisions locked (from review)

1. Command: `lore` dispatcher, `lore docsets` subcommand. (Q1/Q2)
2. Remove `OPENLORE_DOCSETS`; per-session in-memory state. (Q3)
3. Two `CmdContext` accessors: `Docsets()`, `PublishTargets()`. (Q4/Q6)
4. `ACCESS` = direct FS writability; `publish_dir` stays a separate inbox concern. (Q5)
5. Aligned table, header, named attribute tokens. (Q7)
6. `approval` as an attribute token, not an `ACCESS` mangling. (Q8)
7. Unenforced mode → synthetic `public` docset at `/` with folder mounts folded in. (Q9/Q11)
8. `lore` bare exits 0; unknown subcommand exits 1. (Q10)
9. Add `Identity.HomeDocset`; `public` home is always false. (Q12)
