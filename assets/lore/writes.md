# Writing to OpenLore

OpenLore is read-only by default — but it can also be a **safe, writable**
knowledge layer your agents and teammates share over SSH. Writes are atomic,
scoped per identity, optionally gated behind human approval, and never give
anyone a real shell. This page covers the write commands; for the design and
internals see `docs/write-system.md` in the repo.

## Is writing even on?

The substrate boots **read-only**. Writing is enabled by the operator
(`readonly: false` globally, or per-identity write scope). If your session can
write, `help` shows a **WRITES** section. If it doesn't, you're read-only — that
is the safe default, not a bug.

> Embedded-docs binaries (docs baked in with `embed.FS`) are read-only by
> construction and can never be made writable.

## The write commands

Everything writes **whole files atomically** — there is no streaming or partial
write. A file either fully updates or it doesn't.

```bash
# Overwrite (redirect) — replaces the whole file
echo "# Notes" > /mydocset/notes.md

# Append — adds to the end, safely, even under concurrent writers
echo "- another point" >> /mydocset/notes.md

# tee — write stdin to a file (and pass it through)
cat input.md | tee /mydocset/copy.md

# write — atomic write from stdin, with optional preconditions
echo "v2" | write /mydocset/notes.md

# patch — apply a unified diff atomically
cat change.diff | patch /mydocset/notes.md

# sed -i — edit a file in place
sed -i 's/old/new/g' /mydocset/notes.md

# mkdir — create a folder INSIDE a docset (not a docset root)
mkdir /mydocset/subsection

# publish — publish stdin as a new source into a docset
echo "# API" | publish mydocset api.md
```

## You can only write where you're scoped

Each identity is confined to the docset roots it's allowed to write (the docsets
it can `publish` to). A write outside your scope is rejected as read-only —
even though you can still *read* shared docsets. This is how a team shares one
server: everyone reads the common lore, but each agent writes only its own
space.

## Concurrent writes are safe (compare-and-swap)

By default OpenLore uses a **hash** conflict policy: an overwrite is a
compare-and-swap against the bytes you read. If someone changed the file since
you read it, your overwrite is **rejected** with a precondition error instead of
silently clobbering their change — re-read and retry.

```bash
# If this fails with "precondition failed", re-read and reapply your change.
echo "$NEW" > /mydocset/shared.md
```

Append (`>>`) and `patch` are *always* safe under concurrency — they read,
modify, and commit-if-unchanged with automatic retry.

Operators can switch a docset to `last_write_wins` (unconditional but still
atomic) if a doc is single-writer and they don't want CAS rejections.

## Some writes need human approval

A docset can require approval for certain paths. When you write such a path, the
change isn't committed — it becomes a **pending request** for a human to review:

```bash
echo "freeze deploys" > /ops/policy.md
#   write to /ops/policy.md pending approval as req_ab12 (requires approve@oncall)
```

Anyone reviewing can inspect and decide:

```bash
ls /requests                 # list pending requests
cat /requests/req_ab12       # see who proposed what, plus a diff
approve req_ab12             # commit it (needs the right capability)
reject req_ab12              # discard it
```

## Async external work: `spawn`

Trusted identities (those granted the `spawn` capability) can run a slow external
command and have its output written back into the lore **for everyone**, without
blocking:

```bash
spawn --writes /channel/infra/temporal-ns/live.md -- \
    sh -c 'kubectl apply -f ns.yaml && kubectl get all -n temporal -o yaml'
#   Spawned job_84f3 — writing to /channel/infra/temporal-ns/live.md when done
#     track: /jobs/job_84f3
```

The command runs in the background; check its state any time:

```bash
ls /jobs                  # all jobs
cat /jobs/job_84f3        # running | done | failed, target, timestamps, detail
```

The write-back goes through the **same** rules as any other write — it's scoped,
CAS-checked, and approval-gated. `spawn` fails immediately if the target is
outside your scope. Jobs are in-memory: a server restart drops in-flight work
(fine for ad-hoc operational tasks; use a normal write for anything durable).

## What you still can't do

OpenLore is not a shell host. There's no `rm`, `mv`, `cp`, `chmod`, `wget`,
`curl`, or arbitrary process execution from a normal session. The only mutation
surface is the whole-file write verbs above, `mkdir` inside docsets, and (for
explicitly trusted identities) `spawn`. The shell is an interpreter, not bash.

See also: `cat /auth.md` for identities and capabilities, and the repo's
`docs/write-system.md` for the full internal design.
