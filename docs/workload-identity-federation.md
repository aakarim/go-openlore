# Setting up Workload Identity Federation (WIF)

**Status:** Shipped (Phase 4). The `jwt-bearer` grant verifies external IdP
assertions against each configured issuer's JWKS and exchanges them for OpenLore
tokens. Bearer tokens minted without WIF carry the `full` scope and continue to
verify unchanged — WIF is additive, never a breaking change.

**Audience:** operators configuring an OpenLore instance to accept federated
workloads on the HTTP/MCP transports.

**See also:**
- [`mcp-bearer-auth.md`](./mcp-bearer-auth.md) — the full auth design; WIF is
  §8.1 (token endpoint / `jwt-bearer` grant), §10 (config schema), and Phase 4
  of §11.
- [`teleport-oidc-ssh-integration.md`](./teleport-oidc-ssh-integration.md) — the
  **SSH-transport** analogue (short-lived SSH certificates via Teleport / native
  OIDC-over-SSH). WIF here covers HTTP/MCP; that doc covers SSH.
- Anthropic's [Workload Identity Federation docs](https://platform.claude.com/docs/en/manage-claude/workload-identity-federation).
- The served, end-user version of this guide:
  [`../openlore.sh/docs/workload-identity-federation.md`](../openlore.sh/docs/workload-identity-federation.md)
  (embedded and reachable as `cat /workload-identity-federation.md` on a running
  server).

---

## What WIF gives you

Workloads (CI runners, agents, pods) authenticate to `/mcp` and `/api` **without
a long-lived secret**. A workload presents a short-lived JWT its platform already
issues (GitHub Actions OIDC, Kubernetes/SPIFFE, Okta/Entra/Keycloak); OpenLore
verifies it against the IdP's JWKS, matches its claims to a rule, and exchanges
it for a short-lived OpenLore bearer token bound to a named identity.

```
 platform JWT ──POST /oauth/token (grant=jwt-bearer, assertion=<jwt>)──▶ OpenLore token
      │                                                                       │
      │  verify vs IdP JWKS → match claims to rule → rule→identity+scope      │
      ▼                                                                       ▼
  no OpenLore secret stored on the workload                     Authorization: Bearer …
                                                                 → /mcp and /api, scoped
                                                                   to the identity's lore
```

The exchanged token flows through the **same** `Issuer` and
`IdentityStore.Resolve` as human passkey logins, and resolves to the **same**
`Identity` (lore, capabilities, home) used by SSH sessions — so a federated
caller is scoped by `FilteredView` exactly like an SSH session.

## Prerequisites

- The identities the rules resolve to already exist in `lore.json`
  (`identities[]` with a `lore`, optional `publish`/`capabilities`/`home`). WIF
  maps *external claims* onto these existing identities; it does not create them.
- The HTTP server is enabled (`http_port`), since `/oauth/token`, `/mcp`, and
  `/api` are mounted there.
- Posture derives from `allow_keyless` / `unknown_identity` — there is no
  separate WIF enable switch. If SSH is public, anonymous MCP/HTTP still works
  (read-only `default` lore); WIF simply lets a workload upgrade to a named
  identity.

## Configuration

```yaml
# openlore.yml
auth:
  tokens:
    issuer: https://openlore.example      # `iss` on issued tokens + JWKS base
    audience: https://openlore.example    # one audience per instance
    access_ttl: 30m
    refresh_ttl: 720h
    # ES256 signing key auto-generated in DataDir; the Issuer interface lets a
    # DB-backed deployment (knowledge-backend) inject a shared key instead.

  # Identity resolution rules. Exact-`sub` rules serve human/passkey/SSH
  # identities; WIF rules add claim/prefix matches, a narrowing scope, and a ttl.
  rules:
    - match: { sub: "alice" }             # human identity (lore from the table)

    - match:                              # WIF rule
        sub_prefix: "repo:my-org/my-repo:"
        aud: "https://openlore.example"
      identity: ci-indexer                # must exist in lore.json identities[]
      scope: "read"                       # narrows below the identity's authority
      ttl: 15m                            # caps this rule's OpenLore token TTL

  # External IdPs whose JWTs may be exchanged.
  oidc_issuers:
    - issuer_url: https://token.actions.githubusercontent.com
      jwks: { mode: discovery }           # discover keys from the issuer
```

### Rule matching

| Key | Meaning |
|---|---|
| `sub` | Exact subject match. |
| `sub_prefix` | Prefix match on `sub` (e.g. pin a repo without pinning every branch). |
| `aud` | Required audience; pin it to this instance to stop cross-service token reuse. |
| `claims` | Require specific claim values (`environment`, `ref`, `iss`, …) to gate privilege. |

All fields in a rule must match (AND). **Exact `sub` matches take precedence**
over `sub_prefix`/`aud`/`claims` pattern matches, so a specific subject can
override a broad rule. A single `lore.json` identity can back several rules at
different privilege levels (e.g. read-only for PR builds, publish for `main`).

**Verification already binds issuer + audience.** Before any rule is consulted,
the assertion is verified against a *specific* trusted issuer's JWKS (so its
`iss` is authentic) and its `aud` must equal this instance's `audience` (so a
token minted for another service can't be replayed here). If you trust **more
than one** `oidc_issuer` and their `sub` namespaces could overlap, pin the
issuer explicitly with `claims: { iss: "https://…" }` so a token from issuer B
can't satisfy a rule you wrote for issuer A.

### Scopes (narrow-only, fail-closed)

Effective authority = `identity_authority ∩ scope`.

- `full` — full identity authority (what SSH keys / passkey logins resolve to).
  Reserved and exclusive; if any narrowing scope is present, `full` is ignored.
- A narrowing scope (e.g. `read`) constrains the identity's authority.
- Missing / empty / unrecognized scope → **denied** (never full).

Forward-compat: today every non-WIF token carries `full`; WIF introduces
narrowing scopes without changing how existing `full` tokens resolve.

### Token lifetime when brokering

An exchanged token's lifetime is
`min(rule ttl, 2 × remaining assertion lifetime, access_ttl)`, floored at 60s — a
federated token never long-outlives the assertion that produced it. The
`jwt-bearer` exchange issues an **access token only** (no refresh token): a
workload re-presents a fresh platform assertion each time, so no long-lived
credential is ever created. `refresh_ttl` applies only to human
(`authorization_code`) logins.

## Platform quick reference

- **GitHub Actions** — job needs `permissions: id-token: write`; request the
  OIDC token with `&audience=<your audience>`, then exchange it. `sub` looks like
  `repo:org/repo:ref:refs/heads/main` (use `sub_prefix: "repo:org/repo:"`).
- **Kubernetes / SPIFFE** — project a service-account token with `audience:
  <your audience>` (or use a SPIRE-issued SVID); match on
  `system:serviceaccount:<ns>:<name>` or the SPIFFE ID; point `issuer_url` /
  `jwks` at the cluster or SPIRE bundle.
- **Okta / Entra / Keycloak** — register the issuer under `oidc_issuers`; match
  on `sub` / group / app claims.

Full runnable exchange snippets for each platform are in the served guide:
[`../openlore.sh/docs/workload-identity-federation.md`](../openlore.sh/docs/workload-identity-federation.md).

## Verifying the setup

1. From the workload, obtain the platform JWT and exchange it:
   ```bash
   curl -sS -X POST https://openlore.example/oauth/token \
     -d grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer \
     -d "assertion=$PLATFORM_JWT"
   # → {"access_token":"…","token_type":"Bearer","expires_in":900,…}
   ```
2. Call `/api` with the token and confirm the identity:
   ```bash
   curl -sS -X POST https://openlore.example/api/shell \
     -H "Authorization: Bearer $OL_TOKEN" \
     -H 'Content-Type: application/json' \
     -d '{"command":"whoami"}'
   # → resolves to the rule's identity + lore, not "anonymous (lore: default)"
   ```
3. Confirm scoping: a `read`-scoped token can `cat` its docsets but a write
   (`publish`, `>`) is rejected read-only; docsets outside its lore are absent.
