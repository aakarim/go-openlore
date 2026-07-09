# Temporary SSH Logins via OIDC / Workload Identity Federation (Teleport)

**Status:** Draft / design
**Scope:** How OpenLore grants *short-lived, identity-federated* SSH access to
humans and workloads (CI, agents) — using Teleport as the certificate issuer,
plus a path for native OIDC-over-SSH without Teleport.

---

## 1. The problem

OpenLore today authenticates SSH clients with **long-lived material**:

- a raw SSH public key registered in `lore.json` (`identities[].public_key`), or
- an SSH certificate signed by a CA we trust (`ca_keys_file` →
  `wish.WithTrustedUserCAKeys`), whose `ValidPrincipals` map to a lore, or
- keyless (accept-all) mode for fresh containers / CI.

This is the SSH-key-distribution problem that OpenAI's
[workload identity federation](https://developers.openai.com/api/docs/guides/workload-identity-federation)
and Teleport both exist to kill. We want:

- **No long-lived keys** handed to agents or CI runners.
- **Short-lived logins** that expire in minutes/hours, revoked passively by
  expiry.
- **Identity from an IdP** (GitHub Actions OIDC, Okta/Keycloak/Entra,
  Kubernetes/SPIFFE) mapped to a lore + capabilities.
- Works **over SSH**, which is OpenLore's primary transport. (HTTP/MCP can do
  `Authorization: Bearer <jwt>` trivially; SSH is the interesting case.)

---

## 2. How Teleport does temporary SSH logins (reference model)

Teleport is the canonical implementation of "temporary SSH login from an IdP".
The mechanism is **the SSH certificate**, not a custom protocol:

```diagram
                    ┌──────── humans ────────┐   ┌──── workloads/CI ────┐
                    │ tsh login --proxy=...   │   │ tbot (Machine ID)    │
                    │  → OIDC/SAML redirect   │   │  → platform join     │
                    │  → IdP authenticates    │   │    (github/aws/k8s)  │
                    └───────────┬─────────────┘   └──────────┬──────────┘
                                ▼                             ▼
                    ╭───────────────────────────────────────────────╮
                    │           Teleport Auth Service (CA)            │
                    │  claims_to_roles → role.allow.logins            │
                    │  issues SHORT-LIVED SSH user cert:              │
                    │    ValidPrincipals = allowed logins            │
                    │    ValidBefore     = now + max_session_ttl     │
                    │    Extensions      = teleport-roles, …         │
                    ╰───────────────────────┬───────────────────────╯
                                            ▼
                    client presents cert ───────────────▶ SSH server
                                            trusts Teleport user CA via
                                            TrustedUserCAKeys; validates
                                            signature + time window +
                                            ValidPrincipals
```

Key properties (all confirmed from Teleport docs/source):

1. **Auth Service = SSH user CA.** After SSO, it mints a short-lived OpenSSH
   user certificate. TTL is governed by role `max_session_ttl`; shortest role
   TTL wins. Teleport's whole model is "certs valid for hours-to-minutes,
   prefer expiry over revocation lists."
2. **IdP claims → roles → principals.** OIDC connector `claims_to_roles` maps an
   IdP group claim to Teleport roles; a role's `allow.logins` become the SSH
   cert's `ValidPrincipals`.
3. **Plain OpenSSH servers just trust the CA.** Teleport's own OpenSSH guide
   tells you to export the CA pubkey and add `TrustedUserCAKeys
   /etc/ssh/teleport_openssh_ca.pub` to `sshd_config`. Nothing Teleport-specific
   is required on the node for the basic case.
4. **Teleport-specific data rides in cert extensions** (`teleport-roles`, custom
   `cert_extensions`). A node *may* read these but OpenSSH semantics
   (`ValidPrincipals`, validity window, critical options) are enough.
5. **Workloads use Machine ID / `tbot`.** `tbot` authenticates to Teleport with
   a **platform-bound join token** (`join_method: github`/`aws`/`kubernetes`/…)
   — i.e. the same OIDC/platform-attestation idea as OpenAI WIF — and writes a
   short-lived `key` + `key-cert.pub` to disk. CI then SSHes with that cert. No
   long-lived secret in the runner.

**The punchline for OpenLore:** we are the *relying SSH server*. Teleport is the
issuer. OpenLore already does step 3 + 4 partially. The work is hardening,
mapping, lifetime handling, and docs.

---

## 3. What OpenLore already supports

| Teleport concept            | OpenLore today                                                                 |
|-----------------------------|-------------------------------------------------------------------------------|
| Trust the user CA           | `config.CAKeysFile` → `wish.WithTrustedUserCAKeys` (`server.go` `ListenAndServe`) |
| Principal → access mapping  | `resolveIdentity` maps `cert.ValidPrincipals` to a `lore` name (`server.go` L344-353) |
| Cert deny for unknown ident | `PublicKeyAuth` allows certs whose principal is a known lore (`server.go` L686-693) |
| Host cert                   | `config.HostCertFile` (server presents a CA-signed host key)                   |

So a **minimal Teleport integration already works operationally**: point
`ca_keys_file` at Teleport's exported user CA, name Teleport role logins to match
lore names, done. The rest of this doc is what we must *add* to make it correct,
safe, and ergonomic.

---

## 4. Gaps & risks in the current implementation

These must be fixed before advertising federated SSH access:

1. **`WithTrustedUserCAKeys` overrides our own public-key handler (the real
   blocker).** `ssh.PublicKeyAuth` is a *setter* (`srv.PublicKeyHandler = fn`),
   and `wish.WithTrustedUserCAKeys` is implemented as a `PublicKeyAuth` setter.
   In `ListenAndServe` the `CAKeysFile` option is appended **last**, so enabling
   `ca_keys_file` silently replaces OpenLore's own handler — the
   `identities[].public_key` matching and the deny-check in `PublicKeyAuth`
   become dead code. Consequences:
   - `allow_keyless: false` + `ca_keys_file`: a raw registered key (non-cert) is
     rejected (`CertChecker` returns false for non-certs) — **raw-key identities
     can't log in alongside certs.**
   - `allow_keyless: true` + `ca_keys_file`: a raw key fails publickey, falls
     back to the always-true keyboard-interactive handler, and is admitted with
     **no key recorded → demoted to `default` lore.**
   **Fix:** compose a single `PublicKeyHandler` ourselves — try CA-cert
   verification via our own `gossh.CertChecker` first, then fall back to
   raw-key/identity matching — instead of relying on `WithTrustedUserCAKeys` to
   layer on top. This is a prerequisite for letting Teleport certs and raw keys
   coexist.
2. **Forged-principal isolation bypass in keyless-without-CA (minor).** When
   `allow_keyless: true` **and no `ca_keys_file`**, the active handler accepts
   any key, so `resolveIdentity` will trust `cert.ValidPrincipals` from a
   self-signed cert to pick a lore. This is *not* an escalation when a CA is
   configured (the CA checker rejects forged certs), and keyless already means
   "anyone may connect" — but it does defeat per-lore isolation for anonymous
   users. **Fix:** only honor cert principals when the cert was CA-verified;
   ignore principals entirely in keyless mode.
3. **No explicit TTL / validity enforcement visibility.** `wish` +
   `TrustedUserCAKeys` validates `ValidAfter/ValidBefore` during the handshake,
   but we never surface it, never enforce a *max acceptable* TTL, and never log
   expiry. **Fix:** read `cert.ValidBefore`, reject certs whose remaining
   lifetime exceeds a configurable `max_cert_ttl` (defense against
   over-long-lived certs), and log expiry on connect.
4. **No CA rotation story.** `ca_keys_file` is a single file read at startup.
   Teleport rotates CAs. **Fix:** support multiple CA keys in the file (already
   possible — multiple lines) and document the dual-publish rotation window;
   optionally hot-reload the file.
5. **No claim/role → capability mapping.** OpenLore has `capabilities` (approval
   gating) on `identities`, but cert-authenticated users get none. **Fix:** map
   cert principals (and optionally Teleport `teleport-roles` extension) to
   capabilities.
6. **No audit trail of *which IdP identity*.** We log principals but not the
   upstream subject. **Fix:** surface cert `KeyId` (Teleport puts the username
   there) and selected extensions into the connect log / metrics.

---

## 5. Design — Model A: Teleport as issuer (recommended)

OpenLore stays a dumb, correct relying party. Teleport owns identity, RBAC, TTL.

### 5.1 Trust anchor

```yaml
# openlore.yml
auth:
  ssh_ca:
    # One or more trusted user-CA public keys (OpenSSH authorized_keys format).
    # During CA rotation, list both old and new.
    trusted_user_ca_keys: ./teleport_user_ca.pub
    max_cert_ttl: 8h          # reject certs valid longer than this
    require_ca_for_principals: true  # never trust ValidPrincipals from a
                                     # non-CA-verified cert (closes gap #1)
```

Operator gets the CA key from Teleport:

```bash
curl 'https://<proxy>/webapi/auth/export?type=openssh' \
  | sed 's/cert-authority //' > teleport_user_ca.pub
```

### 5.2 Principal → lore + capability mapping

Reuse the existing `lore` map; add an explicit federated mapping so principals
don't have to be named identically to lores and can grant capabilities:

```yaml
# lore.json (or openlore.yml auth section)
federated_principals:
  - principal: "docset:engineering"     # exact, or "agent:*" glob
    lore: "engineering"
    capabilities: []
  - principal: "openlore-approver"
    lore: "engineering"
    capabilities: ["approve@oncall"]
```

Resolution order in `resolveIdentity`:
1. raw public-key identity match (unchanged), else
2. **CA-verified** cert → first matching `federated_principals` rule (glob), else
3. **CA-verified** cert → `ValidPrincipals` that directly names a lore
   (current behavior, kept for convenience), else
4. `default` lore.

Steps 2–3 require `ctx` to carry a "cert was CA-verified" flag.

### 5.3 Optional: read Teleport extensions

If present, log `cert.Extensions["teleport-roles"]` and `cert.KeyId` for audit.
Do **not** require them — keep OpenSSH-native semantics so any CA (not just
Teleport) works.

### 5.4 Teleport-side config (operator runbook, goes in README/skill)

```yaml
# Teleport OIDC connector: IdP groups → Teleport roles
kind: oidc
spec:
  claims_to_roles:
    - {claim: groups, value: /eng,   roles: [openlore-eng]}
    - {claim: groups, value: /oncall, roles: [openlore-approver]}
---
# Teleport role: grants the SSH principal OpenLore maps to a lore
kind: role
metadata: {name: openlore-eng}
spec:
  options: {max_session_ttl: 1h}
  allow:
    logins: ["docset:engineering"]   # becomes cert ValidPrincipals
    node_labels: {'openlore': 'true'}
```

Human flow: `tsh login` → `tsh ssh docset:engineering@openlore-host` (or plain
`ssh` using the cert tbot/tsh wrote).

---

## 6. Workloads & CI — the WIF part (Teleport Machine ID / `tbot`)

This is the direct analogue of OpenAI WIF: a platform-attested join, not a
stored secret.

```diagram
GitHub Actions job
  │  OIDC token (repo-bound)         join_method: github
  ▼
tbot ──────────────▶ Teleport Auth ──────────▶ writes ./certs/key, key-cert.pub
  │                  (verifies repo claim,        (short TTL, principal
  │                   issues bot cert)             = docset:ci-indexer)
  ▼
ssh -i ./certs/key -o CertificateFile=./certs/key-cert.pub \
    docset:ci-indexer@openlore-host "grep -r foo /openlore"
        │
        ▼  OpenLore validates cert vs Teleport CA → lore "ci-indexer"
```

Teleport bot/token (operator side):

```yaml
kind: token
spec:
  roles: [Bot]
  join_method: github
  bot_name: openlore-ci
  github:
    allow: [{repository: my-org/my-repo}]
```

OpenLore side: nothing new beyond Model A — the bot's cert principal flows
through the same `federated_principals` mapping. CI holds **zero** long-lived
SSH keys.

---

## 7. Model B: native OIDC-over-SSH (no Teleport dependency)

For users who don't run Teleport, we still want IdP-federated SSH. SSH has no
native "bearer token" auth, so there are three realistic options:

### Option B1 — Built-in CA broker (recommended native path)
OpenLore (or a tiny sidecar `openlore-ssh-broker`) exposes an HTTP endpoint that
**exchanges an OIDC JWT for a short-lived SSH certificate**, mirroring OpenAI's
token-exchange:

```diagram
agent ──POST /ssh/cert  Authorization: Bearer <oidc-jwt>──▶ broker
                          verify iss/aud/sub vs JWKS,
                          match claim rules → principal+TTL,
                          sign ephemeral pubkey with OpenLore user CA
agent ◀── { certificate, ttl } ───────────────────────────┘
agent ──ssh with cert──▶ OpenLore (trusts its own user CA)  ✓
```

- Reuses Model A's relying-party code unchanged (OpenLore just trusts a CA —
  here, its own).
- Broker is the only new component: JWKS fetch+cache (refresh on `kid` miss),
  `iss`/`aud`/`sub`-glob matching à la OpenAI service-account mappings, CEL-free
  exact/glob claim rules, sign with an in-process SSH CA key.
- Library: `github.com/coreos/go-oidc/v3` for discovery/verify; `x/crypto/ssh`
  for cert signing.

### Option B2 — JWT via keyboard-interactive
Carry the JWT in an SSH `keyboard-interactive` response and verify it in the
auth callback. Works without certs, but: clunky UX, token size limits, no
standard client support, and we'd reinvent session lifetime. **Not recommended**
except as a fallback for clients that can't use certs.

### Option B3 — JWT in the SSH username
e.g. `ssh jwt:<base64url-token>@host`. Hacky, length-limited, leaks token into
logs. **Rejected.**

**Recommendation:** B1. It keeps SSH-cert semantics identical to Teleport, so
Model A and Model B share one verification codepath; only the *issuer* differs.

---

## 8. Config schema changes (summary)

```yaml
auth:
  ssh_ca:
    trusted_user_ca_keys: <path>     # multi-key file; rotation = dual list
    max_cert_ttl: <duration>         # reject over-long certs
    require_ca_for_principals: true  # security fix for gap #1
  federated_principals:              # principal-glob → lore + capabilities
    - {principal: <glob>, lore: <name>, capabilities: [<cap>...]}
  oidc_broker:                       # Model B only
    enabled: false
    issuers:
      - url: https://token.actions.githubusercontent.com
        audience: https://openlore/<id>
        claim_rules:
          - {match: {sub: "repo:my-org/my-repo:*"}, principal: "docset:ci", ttl: 15m}
    signing_ca_key: <path>           # the user CA OpenLore also trusts
```

Code touch points:
- `internal/config/config.go`: new `SSHCAConfig`, `FederatedPrincipal`,
  `OIDCBrokerConfig` types + YAML/Option wiring.
- `pkg/openlore/server.go`: record CA-verification on `ssh.Context`; gate
  principal mapping (`resolveIdentity`) on it; enforce `max_cert_ttl`; map
  principals → capabilities; log `KeyId`/extensions.
- `pkg/openlore/identity.go`: add `Capabilities` from federated mapping (field
  already exists).
- (Model B) new `internal/sshbroker/` package + HTTP handler mounted on the
  existing HTTP server.

---

## 9. Implementation phases

1. **Phase 0 — Auth handler rework (must-ship, prerequisite).** Close gap #1:
   replace the `WithTrustedUserCAKeys` reliance with a single composed
   `PublicKeyHandler` that does CA-cert verification *and* raw-key/identity
   matching, so certs and raw keys coexist. Close gap #2: only honor cert
   principals when CA-verified; ignore principals in keyless mode. Add
   `max_cert_ttl` enforcement (gap #3). Tests: raw key + CA cert both work;
   forged cert rejected; principal honored only when CA-verified.
2. **Phase 1 — Teleport relying-party hardening (Model A).** `federated_principals`
   mapping (globs + capabilities), CA multi-key + rotation docs, audit logging
   of `KeyId`/`teleport-roles`. Operator runbook + a `teach`-style skill.
3. **Phase 2 — Workload/CI guide.** Document `tbot` + GitHub/AWS/K8s join;
   example workflow connecting to OpenLore with an ephemeral cert.
4. **Phase 3 — Native broker (Model B1).** OIDC→SSH-cert exchange endpoint;
   JWKS verify + claim rules; reuse Phase 1 verification path.

Phases 0–2 are mostly config + small server changes (the primitive exists).
Phase 3 is the only net-new subsystem.

---

## 10. Open questions

- Should the broker (B1) live **in** OpenLore (one binary, simplest) or as a
  separate signer so the CA private key isn't in the doc-server process?
  (Security argues for separate; ergonomics argue for in-process behind a flag.)
- Do we need active revocation (Teleport-style locks) or is expiry enough? For
  read-only docs, expiry is likely sufficient; revisit when writes are enabled.
- Capability mapping source of truth: cert principals only, or also Teleport
  `teleport-roles` extension (couples us to Teleport)?
- Per-principal TTL ceilings vs one global `max_cert_ttl`.
