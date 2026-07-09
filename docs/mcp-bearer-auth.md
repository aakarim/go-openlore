# Bearer-token Auth & Identity for MCP + HTTP API

**Status:** Design (decisions locked)
**Scope:** How OpenLore authenticates callers of the **MCP-over-HTTP** endpoint
and the **plain JSON HTTP API**, maps each caller to a **named identity**, and
scopes every operation to that identity — via a **login flow** that mints
short-lived bearer tokens. Designed to be **forward-compatible** with OIDC
Workload Identity Federation (WIF); see
[`teleport-oidc-ssh-integration.md`](./teleport-oidc-ssh-integration.md) for the
SSH-transport analogue and Anthropic's
[WIF docs](https://platform.claude.com/docs/en/manage-claude/workload-identity-federation).

---

## 1. Goal & driving use case

A **local Obsidian plugin** must talk to OpenLore's MCP server / HTTP API and act
**as a specific user**:

- A user **logs in** (passkey) and receives a **bearer token**.
- The plugin presents `Authorization: Bearer <token>` to **both** `/mcp` and
  `/api`.
- Every tool call runs with that identity's **lore** (docset access),
  **capabilities** (write/publish/approve), and **home** — exactly as an SSH
  session does today.

**Posture mirrors SSH.** If SSH allows public/anonymous access, so do `/mcp` and
`/api`; if SSH requires a credential, so do they. There is **no separate enable
switch** — posture derives from the existing `allow_keyless` /
`unknown_identity` (§4). A public/anonymous MCP caller behaves like an anonymous
SSH caller: it lands in the **`default` lore**, read-only, and **cannot see
docsets outside that lore** because it runs against the same filtered filesystem
(§6).

**Forward-looking.** Any token we issue fits a WIF-style model where an external
IdP JWT (GitHub Actions, Kubernetes, Okta, SPIFFE, …) is exchanged for an
OpenLore token at the same token endpoint. We build the human path now; WIF is
purely additive later (§8).

---

## 2. What exists today

| Concern | State |
|---|---|
| MCP-over-HTTP (`/mcp`) | `mcp.NewStreamableHTTPHandler` in [`server.go`](../pkg/openlore/server.go); **unauthenticated** |
| JSON HTTP API (`/shell`, `/commands`) | [`mcp_http_api.go`](../pkg/openlore/mcp_http_api.go); **unauthenticated**, single persistent in-memory session |
| MCP shell tool | Runs against the full `s.fs` — **no per-identity scoping** |
| Passkey login | [`/passkey/login/*`](../internal/passkeys/passkeys.go) runs WebAuthn and, on success, sets an HMAC **cookie** carrying a lore. A login that sets identity already exists for the browser. |
| Passkey registration | Invite-based: `passkey register --name X --lore Y` prints a 5-min link ([command.go](../internal/passkeys/command.go), [pending.go](../internal/passkeys/pending.go)) |
| Identity model | `resolveIdentity` ([server.go](../pkg/openlore/server.go)) maps an SSH key/cert → `Identity{LoreName, PathAccess, PublishDocsets, Capabilities, HomeDir}`; matched **only by `public_key`** ([config.go:177](../internal/config/config.go)) |
| FS scoping | The SSH `shellHandler` runs the shell against `s.merge.FilteredView(loreDocsets)` — out-of-lore files are absent from the namespace |
| Libraries | `github.com/golang-jwt/jwt/v5` in the module graph; go-sdk ships an `auth` package (`RequireBearerToken`, `TokenInfo`) |
| knowledge-backend | Imports go-openlore as a library (`replace … => ../go-openlore`) and uses **SQLite** — so new auth storage must be interface-backed |

Two facts drive the design:
1. The go-sdk has a first-class bearer hook. `auth.RequireBearerToken(verifier)` stores `*auth.TokenInfo` on the request context, and the Streamable transport already reads `auth.TokenInfoFromContext(req.Context())`. Identity propagates to tool handlers for free — the bespoke `internal/mcpserver` `AuthResolver`/`WithAuthIdentity` scaffolding is **deleted** in favor of this.
2. The MCP shell tool does **none** of the per-identity scoping the SSH shell does. "Operate as a user" is mostly *that* gap (§6), not the token.

---

## 3. Decisions (summary)

| # | Decision |
|---|---|
| Q1 | **Reference tokens** — carry only `sub`/`iss`/`aud`/`exp` (+`scope`); authority resolved **live** per request |
| Q2 | `sub` = identity `Name`; `public_key` becomes optional; **one identity table** |
| Q3 | Build an OAuth **token endpoint** now (`authorization_code` via passkey); full OAuth trimmings later |
| Q4 | **Rule-based** identity resolution from day one (exact-`sub` = a degenerate rule) |
| Q5 | **Short access token + refresh token** (OAuth/WIF-native) |
| Q6 | Passkey / SSH key / WIF subject are **authenticators**; authority lives in the identity table |
| Q7 | **Flat-file refresh store + rotation** (reuse-detection), behind an interface |
| Q8 | **ES256 + published JWKS** (asymmetric), behind an `Issuer` interface |
| Q9 | **Coarse `Resolve(claims) → Identity`** `IdentityStore` interface |
| Q10 | ES256 + `DataDir`-default key (interface-injectable) + `kid` rotation |
| Q11 | **Invite-based** `passkey register --identity <name>` |
| Q12 | **One audience per instance**; scopes = explicit **`full`** sentinel, fail-closed, narrow-only |

---

## 4. Posture is derived from SSH (`allow_keyless` / `unknown_identity`)

`s.auth` is loaded once at startup ([server.go:98](../pkg/openlore/server.go)) but
read **live per request** by the resolver. The middleware in front of `/mcp` and
`/api` is chosen from the *existing* SSH posture — no new flag:

| `lore.json` | SSH behavior | MCP/HTTP wrapper |
|---|---|---|
| no auth config | full access (local `openlore .`) | **no middleware** — open, full FS |
| `allow_keyless: true` (default) | anyone connects; unknown → `default` lore | **optional-token**: verify a token if present (reject if invalid); if absent → anonymous `default` lore |
| `allow_keyless: false`, `unknown_identity: allow` | any key accepted; unknown → `default` | **required-token**; valid token, unknown `sub` → `default` |
| `allow_keyless: false`, `unknown_identity: deny` | only registered keys | **required-token**; valid token, unknown `sub` → **403** |

Invariants in every posture:
- A **present but invalid** token (bad signature / expired / wrong `aud`) is
  **always rejected**, even in keyless mode.
- A **missing** token is OK only where anonymous SSH is OK.
- Whatever identity results — named or anonymous `default` — is then **scoped**
  by §6. Anonymous ≠ unrestricted; it is `default`, read-only.

Implemented as a small `optionalBearer(verifier)` wrapper alongside the SDK's
`RequireBearerToken`, selected by `allow_keyless`.

**OAuth is advertised even in keyless mode (so Claude always has a login path).**
Optional-token means a *direct* tokenless request (curl, CI, SSH-style clients)
is still served anonymously — we never force OAuth on those. But OAuth-native MCP
clients (Claude Desktop, Cowork) decide whether to offer a login flow by
**discovering protected-resource-metadata**; if we never advertise it, such a
client just connects anonymously and the user has **no in-client way to log in**
(they'd have to paste a token by hand). So the server **advertises** OAuth
metadata in every posture where login is possible. A client that runs the flow
lands on the authorize screen, which itself offers the **public-vs-login choice**
(§8.4): "continue with public access" mints a token that resolves to the same
anonymous `default` identity, so the OAuth flow **completes for everyone** —
authenticated or not — instead of stranding users who only want public docs.

---

## 5. Token model

### 5.1 Reference tokens (Q1), resolved live (Q4)

Access tokens are standard JWTs carrying **only identity**, not authority:

```json
{
  "iss": "https://openlore.example",
  "sub": "alice",                 // identity Name (Q2)
  "aud": "https://openlore.example",  // one audience per instance (Q12)
  "exp": 1735693200,              // short-lived (Q5)
  "iat": 1735689600,
  "scope": "full"                 // sentinel; see §5.4
}
```

On **every request**, the verifier resolves `token → claims → Identity` via the
rule engine (§7). Nothing about permissions is frozen into the token, so
**permission changes take effect on the next request with no re-auth** — editing
a rule/identity is picked up live (subject to config reload; see §9). The only
thing bounded by TTL is a *claim the matcher reads that changed upstream* — the
WIF case, where an IdP attribute change waits for the next short-lived refresh.

### 5.2 Access + refresh (Q5)

- **Access token:** ES256 JWT, short TTL (default ~30 min), stateless, verified
  on the hot path (signature + rule resolution, no store lookup).
- **Refresh token:** opaque, **stateful** (§ interfaces), rotated on every use.
  Presenting an already-used refresh token signals theft → revoke the whole
  chain for that `sub`. Revocation = expiry + delete the refresh row (no
  per-request denylist).

### 5.3 Signing: ES256 + JWKS (Q8, Q10)

- **ES256** (compact — matters for MCP header size; universally supported by OIDC
  libs).
- Private key generated on first boot into `DataDir` (mirrors host-key handling),
  **behind the `Issuer` interface** so knowledge-backend can supply a
  DB-stored, cross-instance keypair.
- Public keys published at a **JWKS endpoint** (`/.well-known/jwks.json`),
  `kid`-tagged; sign with newest, verify against any published; dual-publish
  rotation so in-flight tokens stay valid. Because tokens are asymmetric, any
  resource server (including knowledge-backend) can verify them without a shared
  secret.

### 5.4 Scopes: explicit `full` sentinel (Q12)

Scopes exist to *narrow* a token below its identity's authority (WIF's per-rule
`oauth_scope`), but are not enforced beyond the sentinel today:

- **`scope: "full"`** = full identity authority. Every token today carries it.
- **Missing / empty / unrecognized scope → deny** (fail-closed) — never full.
- **Effective authority = `identity_authority ∩ scope_constraints`**, where
  `full` = ⊤ (no constraint).
- **`full` is reserved** (no future WIF scope may reuse it) and **exclusive**: if
  any narrowing scope is present, `full` is ignored (narrowing wins).

Forward-compat: WIF later issues tokens with *narrowing* scopes instead of
`full`; the verifier's `full → ⊤, unknown → deny, else intersect` logic is stable
and already in place. Old tokens (all ≤ access-token TTL, re-minted from refresh
tokens under current logic) keep resolving to full authority. Additive, not
breaking.

---

## 6. Scoping the MCP shell to the identity (the core work)

**This is "behave the same way as SSH."** The SSH `shellHandler` builds a
**per-identity filesystem** and runs the shell against it, so files outside the
caller's lore are physically absent. The MCP shell tool skips all of it and runs
against the full `s.fs` — the security gap.

The SSH path builds this chain ([server.go](../pkg/openlore/server.go)):

```
sessionFS := s.merge                                    // full merged FS
if id.LoreName != "": sessionFS = s.merge.FilteredView(loreDocsets)  // ← the gate
sessionFS = newApprovalFS(sessionFS, …)                 // Part C approval gating
sessionFS = newScopedWriteFS(sessionFS, writableRoots)  // Part B write isolation
sessionFS = sessionFSFn(id, sessionFS)                  // optional decorator
sessionFS = newReadTrackingFS(sessionFS)                // CAS
shell := shell.NewShell(sessionFS)
shell.SetAllowedActions(...)                            // read-only vs write
shell.SetEnv("OPENLORE_IDENTITY"/"LORE"/"DOCSETS"/"CAPABILITIES"/"HOME"/"USER")
```

`FilteredView(loreDocsets)` is the enforcement: an anonymous caller resolves to
`default` and gets a FilteredView of only the default docsets — it literally
cannot `cat`, `grep`, or `ls` anything else.

**Extract this whole chain into a shared `buildSessionShell(id Identity) *shell.Shell`**
and call it from (1) the SSH `shellHandler`, (2) the MCP `shell` tool handler,
(3) the JSON API. The MCP handler reads `auth.TokenInfoFromContext(ctx)` →
`Identity` (named or anonymous `default`) → `buildSessionShell`. Result: **every**
MCP/HTTP caller, including anonymous public ones, is read-only unless its identity
has write capability and sees only its lore's docsets. Identical to SSH.

### 6.1 Per-request in-memory session for the JSON API

The Streamable transport propagates the request ctx (hence `TokenInfo`) to the
tool handler automatically. The JSON API's current **persistent** in-memory
session does not: a per-call ctx passed to `session.CallTool` does not cross the
in-memory pipe (the handler ctx derives from the session's `Connect` ctx).

**Decision: the JSON API creates a per-request in-memory session.** The
middleware resolves identity, then `server.Connect(ctxWithTokenInfo, …)` is
called **per request**, so the shared tool handler sees the identity via the same
`auth.TokenInfoFromContext` path as the Streamable transport. Identity always
enters via the connection ctx — never via client-supplied tool arguments (a
direct MCP client could spoof those). The persistent-session field on
`MCPHTTPAPI` is removed. `NewInMemoryTransports` is an in-process `net.Pipe`, so
the per-request session is cheap.

---

## 7. Identity resolution (Q2, Q4, Q6, Q9)

All credentials are **authenticators** that establish a `sub`; authority lives in
**one identity table**:

```diagram
credentials (authenticate)             authority (one table, matched by rules)
  ssh public_key ┐
  passkey cred   ├──► sub / claims ──► IdentityStore.Resolve() ──► Identity
  WIF sub/claims ┘                       { lore, capabilities, home, publish }
```

- **`sub` = identity `Name`;** `public_key` becomes optional (an identity may have
  an SSH key, a passkey, both, or neither).
- **Rule-based from day one:** resolution is a list of
  `{ match: {sub|sub_prefix|claims}, lore, capabilities }` rules. The human case
  is the degenerate exact-`sub` rule `{match: {sub: "alice"}}`; WIF adds
  `sub_prefix`/`claims` rules with no new resolution path.
- **`IdentityStore` is a coarse single method** (Q9):
  `Resolve(claims map[string]any) (Identity, error)`. go-openlore's default
  implementation reads `lore.json` (identities + their `match` rules) + the passkey store;
  knowledge-backend supplies a SQLite-backed `Resolve`. Keeps the contract tiny
  and lets the DB backend express matching in SQL.
- Passkeys reference an identity by name (Q6): `StoredCredential.Name` = an
  identity `Name`; login resolves that name → full authority. No authority lives
  in the passkey store.

---

## 8. Login & token issuance (Q3, Q11)

### 8.1 The token endpoint (the WIF seam)

Build a standard OAuth **token endpoint** with **grant-type dispatch** and a
shared `Issuer`:

```diagram
                    ╭─────────────── /oauth/token ───────────────╮
  human (now) ──code──▶│ grant=authorization_code → passkey identity │──▶ access+refresh (ES256)
  workload (WIF)──JWT──▶│ grant=jwt-bearer → claims→rule→identity     │──▶ access+refresh
                    ╰──────────────┬──────────────────────────────╯
                                   ▼  one Issuer, one IdentityStore.Resolve
```

- **Now:** the `authorization_code` grant, driven by the existing **passkey
  login** ceremony. Defer heavyweight OAuth (dynamic client registration, full
  metadata) until a real MCP client needs it.
- **WIF (shipped):** the `jwt-bearer` grant (RFC 7523) on the *same* endpoint
  verifies an external IdP JWT against that issuer's JWKS (pinned to a trusted
  issuer + our audience), matches a rule → identity, narrows by the rule's
  `scope`, and mints an OpenLore access token (access-only — no refresh, so no
  long-lived credential). Same issuer, same identity model. See
  workload-identity-federation.md.
- Token lifetime follows the WIF formula when brokering:
  `min(rule ttl, 2× remaining assertion lifetime)`, floor 60s.

### 8.2 Human login UX (Obsidian)

The deliverable is a standard OAuth **authorization-code + PKCE** flow returning
the code through an **HTTP callback** (normal OAuth), not manual copy. A native
client (the Obsidian plugin) opens `GET /authorize` (loopback `redirect_uri`),
OpenLore runs the passkey ceremony, and on success the login-finish hook calls
`Server.CompleteAuthorize` to mint a PKCE-bound code and redirect the browser
back to the client's `redirect_uri?code=&state=`. The client captures the code on
its loopback port and exchanges it (`code_verifier` + `redirect_uri`) at
`/oauth/token` for access+refresh tokens.

The passkey ceremony and the `/authorize` OAuth layer are separate concerns wired
by the `TokenIssuer` seam, so Phase 3's protected-resource-metadata + dynamic
client registration slot in front of the *same* `/authorize` → `/oauth/token`
mint step — MCP clients (Claude Desktop, Cowork) then federate automatically via
`WWW-Authenticate` → protected-resource-metadata → `/authorize` → `/oauth/token`
with no change to the mint path.

### 8.3 Provisioning (Q11)

Invite-based, retargeted from `--lore` to `--identity`:
`passkey register --identity alice` (identity must exist in the table) → 5-min
link → user registers a passkey → thereafter login mints tokens for `alice`. For
**local single-user Obsidian** the operator *is* the user:
`passkey register --identity me`, click, done. No self-service — access is
granted, not claimed; `default` (read-only) is already reachable anonymously.

### 8.4 The authorize screen: public vs login (universal Claude flow)

The OAuth authorize screen presents **two choices**, so the *same* flow serves
both anonymous and authenticated users:

```diagram
   Claude runs OAuth ──▶ ╭──────────── /authorize ────────────╮
                         │  How do you want to connect?         │
                         │                                      │
                         │  [ Continue with public access ]     │──▶ token: sub=anonymous,
                         │  [ Log in with passkey ]             │       resolves to `default`
                         ╰──────────────┬───────────────────────╯       (read-only)
                                        │  passkey → identity
                                        ▼
                            token: sub=<identity> (full authority)
```

- **Continue with public access** → `/oauth/token` mints a normal access+refresh
  token whose `sub` is the reserved **anonymous** subject. It resolves through the
  identity resolver (§7) to exactly the same `anonymousIdentity()` a tokenless
  caller gets: `default` lore, read-only, cannot see other docsets. It is **not**
  more privileged than tokenless anonymous — it simply gives an OAuth-only client
  a token to hold so its connect flow completes.
- **Log in with passkey** → the existing WebAuthn ceremony (§8.2) resolves to a
  named identity; `/oauth/token` mints a token with `sub=<identity>` and that
  identity's full authority.

**Why this matters:** an OAuth-native client (Claude) that discovers OAuth
metadata will insist on completing the flow before connecting — it won't fall
back to a bare anonymous connection. Without the public option, users who only
want public docs would be **unable to connect through Claude at all**. The public
button lets them satisfy the OAuth flow *as anonymous*, so **login works for
everyone**: identity for those who want it, public access for those who don't,
one screen, one flow. Non-OAuth clients (curl, CI) are unaffected — they still
hit the endpoints tokenless and are served anonymously (§4).

The reserved anonymous `sub` is a first-class (if unprivileged) identity in the
token model — the verifier treats a public token like any other, then §7 maps it
to `default`. Nothing downstream special-cases "public": it is just the anonymous
identity wearing a token.

---

## 9. Storage interfaces (Q7, Q8, Q9)

knowledge-backend backs these with SQLite; go-openlore ships flat-file defaults.
Injected via server functional options, defaulting so standalone `openlore` needs
no config.

```diagram
                     pkg/openlore (interface + default)     knowledge-backend
  Issuer            ── mint/verify (ES256), JWKS ──────────  key in DataDir      │ key in DB (shared)
  RefreshTokenStore ── save / lookup / rotate / revoke ────  flat file (DataDir) │ SQLite table
  IdentityStore     ── Resolve(claims) → Identity ─────────  lore.json + passkey │ SQLite tables
```

- **`Issuer`** — signing seam. Default is an **ES256 keypair in `DataDir`**,
  generated on first boot; knowledge-backend injects a DB-stored keypair (host-
  key-derived keys are per-host and break multi-instance).
- **`RefreshTokenStore`** — stateful, revocable; flat-file default, DB impl for
  knowledge-backend.
- **`IdentityStore`** — coarse `Resolve` (Q9); default reads lore.json
  (identities + their `match` rules) + the passkey store, DB impl reads SQL.
  This is what makes "permissions change live" work against either backend.

**Config reload:** "live" is true within a process, but `s.auth` is a startup
snapshot — on-disk edits to lore.json need a reload to apply without
restart (`air` restarts in dev; add an auth reload for production parity; the DB
already has `Reload()`).

---

## 10. Config schema (summary)

Config splits by responsibility: **token issuance is server infrastructure**
(`openlore.yml`), **access policy is per-lore** (`lore.json`). Posture (public vs
token-required) derives from `lore.json`'s `allow_keyless`/`unknown_identity`
(§4) — it is not a token setting.

`openlore.yml` (server infra — sits alongside `passkeys`):

```yaml
tokens:
  issuer: https://openlore.example      # `iss` + JWKS base
  audience: https://openlore.example    # one audience per instance (Q12)
  access_ttl: 30m
  refresh_ttl: 720h
  # ES256 key auto-generated in DataDir; Issuer interface overrides for DB.

# ── reserved for WIF (Q8 / §8.1) ──────────────────────
oidc_issuers:
  - issuer_url: https://token.actions.githubusercontent.com
    jwks: { mode: discovery }
```

`lore.json` (access policy). Identity resolution is **rule-based, but the rules
live on the identity they select** (Q4) — every rule maps to exactly one
identity, so a separate list with a back-pointer is redundant. The exact-`sub`
human case needs no entry (a token whose `sub` == an identity `Name` resolves
there implicitly). WIF adds `sub_prefix`/`aud`/`claims` match entries with
narrowing `scope`/`ttl`:

```json
{
  "identities": [
    { "name": "alice", "lore": "eng" },
    {
      "name": "ci-indexer",
      "lore": "ci",
      "match": [
        { "sub_prefix": "repo:org/repo:", "aud": "https://openlore.example",
          "scope": "read", "ttl": "15m" }
      ]
    }
  ]
}
```

Cross-identity match precedence (when two identities' *patterns* both match) is
exact-`sub`-beats-pattern, then most-specific; moot in Phase 1 (exact-`sub` only,
which is unique).

Code touch points:
- `internal/config/config.go`: `AuthTokensConfig` + `OIDCIssuer` on `Config`
  (openlore.yml); `IdentityMatch` on `AuthIdentity` (lore.json).
- `pkg/openlore/`: `issuer.go` (ES256 + JWKS, `Issuer` iface), `refreshstore.go`
  (`RefreshTokenStore` iface + flat-file impl), `identitystore.go`
  (`IdentityStore` iface + lore.json/passkey/rule impl), `tokenendpoint.go`
  (`/oauth/token` + JWKS handler), refactor `resolveIdentity` to funnel through
  `IdentityStore.Resolve`, extract `buildSessionShell(id)`.
- `pkg/openlore/server.go`: wrap `/mcp` + `/api` with posture-aware middleware
  (§4); per-request in-memory session for the JSON API (§6.1); mount
  `/oauth/token` + JWKS.
- Consolidate the two MCP server constructors into one; delete
  `internal/mcpserver` auth scaffolding.
- `internal/passkeys`: login-success hook that mints via the token endpoint;
  `register` retargeted to `--identity`.
- `cmd/openlore/main.go`: `token` subcommand (debug mint/verify).

---

## 11. Implementation phases

1. **Phase 0 — Consolidate + scope (prerequisite, highest value).** Merge the two
   MCP server constructors; extract `buildSessionShell(id)`; run MCP + JSON API
   through it; switch the JSON API to a per-request in-memory session (§6.1).
   **No tokens yet**, but public MCP/HTTP access now behaves like anonymous SSH:
   `default` lore, read-only, FilteredView FS. Tests: anonymous MCP caller reads
   `default` docsets but **cannot** read a non-default docset; anonymous is
   read-only.
2. **Phase 1 — Issuer + IdentityStore + token endpoint. ✅ Implemented.** ES256
   `Issuer` + JWKS ([`issuer.go`](../pkg/openlore/issuer.go)); flat-file
   `RefreshTokenStore` with rotation + reuse-detection
   ([`refreshstore.go`](../pkg/openlore/refreshstore.go)); coarse
   `IdentityStore.Resolve` (exact-`sub` rules, direct `sub`→identity, reserved
   `sub_prefix`/claims) ([`identitystore.go`](../pkg/openlore/identitystore.go));
   `/oauth/token` with `authorization_code` + `refresh_token` grants and an
   explicit `jwt-bearer` **501 stub** for the WIF seam
   ([`tokenendpoint.go`](../pkg/openlore/tokenendpoint.go)); posture-aware
   middleware on `/mcp` + `/api` ([`auth_middleware.go`](../pkg/openlore/auth_middleware.go));
   `token mint`/`verify` CLI. `public_key` is optional; `sub`=Name; the reserved
   `anonymous` sub resolves to `default`. Token auth activates only when
   `tokens` is configured (openlore.yml) (otherwise Phase 0 anonymous). Tests cover: valid
   token → identity + scope; expired/forged → 401 even keyless; unknown `sub` +
   `unknown_identity: deny` → 403; refresh rotation + reuse revokes the chain.
3. **Phase 2 — Passkey login → token + Obsidian. ✅ Implemented.** A standard
   OAuth **authorization-code + PKCE** flow (RFC 7636) mints tokens via a passkey
   login, returning the code to the client through an **HTTP callback** (normal
   OAuth), not manual copy: `GET /authorize`
   ([`authorize.go`](../pkg/openlore/authorize.go)) validates the request
   (loopback/custom-scheme `redirect_uri` only, to block open redirects) and
   redirects into the existing passkey ceremony; on success the login-finish hook
   calls `Server.CompleteAuthorize`, which mints a PKCE-bound auth code and
   redirects back to `redirect_uri?code=&state=`; `/oauth/token` verifies the
   `code_verifier` + `redirect_uri` and mints access+refresh. `passkey register`
   is retargeted from `--lore` to `--identity` (`StoredCredential.Identity` =
   token `sub`); the passkey↔token seam is the `TokenIssuer` interface
   ([`passkeys.go`](../internal/passkeys/passkeys.go)). Obsidian runbook +
   bearer/OAuth docs in [`assets/lore/mcp.md`](../assets/lore/mcp.md). Tests
   cover the full PKCE round trip, wrong/missing verifier, redirect mismatch, and
   bad `/authorize` params.
4. **Phase 3 — Full OAuth for MCP clients. ✅ Implemented.** OAuth discovery +
   Dynamic Client Registration in front of the same `/authorize` → `/oauth/token`
   mint step, so Claude Desktop/Cowork federate automatically:
   - **RFC 9728 Protected Resource Metadata** at
     `/.well-known/oauth-protected-resource` (`resource` == `tokens.audience`,
     `authorization_servers` == [`tokens.issuer`]) and **RFC 8414 Authorization
     Server Metadata** at `/.well-known/oauth-authorization-server`
     ([`oauth_metadata.go`](../pkg/openlore/oauth_metadata.go)). Both are mounted
     whenever `tokens` is configured — **even in keyless mode** — so OAuth-native
     clients always discover the flow (§4). The metadata's absolute URLs derive
     from `tokens.issuer` (must be pathless).
   - **RFC 7591 Dynamic Client Registration** at `/register`, open, issuing only
     **public PKCE clients** (`token_endpoint_auth_method=none`, no
     `client_secret`) ([`registration.go`](../pkg/openlore/registration.go)),
     persisted behind a **`ClientStore`** interface (flat-file default in
     `DataDir/auth/clients.json`; DB impl for knowledge-backend)
     ([`clientstore.go`](../pkg/openlore/clientstore.go)).
   - **Redirect policy** now DCR-aware: a registered `client_id` must present a
     redirect that **exactly matches** a registered one (this is what admits a
     remote HTTPS callback like `https://claude.ai/...`); an absent/unregistered
     `client_id` falls back to the native rules (loopback / custom scheme only),
     never a remote origin ([`authorize.go`](../pkg/openlore/authorize.go)). Auth
     codes are bound to `client_id` + the RFC 8707 `resource` and re-checked at
     the token endpoint.
   - **Public-vs-login authorize screen** (§8.4): `GET /authorize` renders a
     choice page — "continue with public access" POSTs to `/authorize/public`
     and mints an anonymous `default` (read-only) token so the flow completes for
     every user; "log in with passkey" runs the Phase 2 ceremony and mints an
     identity token. PKCE (`code_challenge`) is now **required** on `/authorize`.
   - **`WWW-Authenticate`** on every 401 carries
     `resource_metadata="…/.well-known/oauth-protected-resource"` so a challenged
     client discovers the resource (RFC 9728 §5.1). Non-OAuth clients (curl, CI)
     still connect tokenless/anonymous.

   Keyless discovery caveat: a keyless server never 401s a tokenless request, so
   OAuth-native clients must discover via proactive
   `/.well-known/oauth-protected-resource` probing (the metadata is always
   advertised). If a client only starts OAuth after a 401, run required-token
   mode. WIF (`jwt-bearer`) is deliberately **not** advertised until Phase 4.
5. **Phase 4 — WIF / OIDC.** `jwt-bearer` grant on `/oauth/token`; `OIDCVerifier`
   (JWKS + claim/`sub_prefix` rules); narrowing `scope` enforcement
   (`intersect`). Reuses Phases 0–2 unchanged. **Docs deliverable:** operator
   setup guide in [`workload-identity-federation.md`](./workload-identity-federation.md)
   (repo reference, cross-links this plan + the Teleport SSH doc) and the served
   end-user guide `openlore.sh/docs/workload-identity-federation.md` (mirrored to
   `assets/lore/`, reachable as `cat /workload-identity-federation.md`); both are
   already written and marked "planned" pending this phase.

Phases 0–2 ship the Obsidian use case. Phases 3–4 are additive and gated behind
config already reserved here.

---

## 12. Open questions

- **Service identities for WIF:** reuse the identity table, or add a distinct
  "service account" concept (closer to WIF's `svac_`)?
- **Instant revocation:** is refresh-token revocation + short access TTL enough,
  or do writes eventually require a small access-token denylist for
  *immediate* cutoff (sub-TTL)?
- **Auth config reload:** add file-watch/`Reload()` for lore.json + openlore.yml now, or
  rely on process restart until multi-instance/DB deployment?
- **Unifying passkey cookie + bearer:** should the browser session cookie and the
  MCP bearer be issued by the same endpoint for one identity?

Resolved (see §3): posture (derived), JSON-API session model (per-request),
token model (reference), resolution (rule-based), lifetime (short+refresh),
signing (ES256/JWKS), storage (interfaces), provisioning (invite), scopes
(`full` sentinel, fail-closed, narrow-only).
