package vfs

import "fmt"

// ChangeAction discriminates the kind of mutation a ChangeSet describes. Every
// mutating WritableFS method has a corresponding action, so every namespace
// mutation — file writes, directory creation, and removals — flows through the
// ordered log as a ChangeSet. Directories are first-class namespace entries
// (like a special filetype), so their creation and removal are logged
// operations too: this is what prevents a write from racing ahead of a remove
// (or landing on an already-removed path) — the single serialized applier
// orders them.
type ChangeAction string

const (
	// ChangeActionWrite is a single-file whole-object write (WriteFileAtomic).
	ChangeActionWrite ChangeAction = "write"
	// ChangeActionMkdir creates a single directory (Mkdir).
	ChangeActionMkdir ChangeAction = "mkdir"
	// ChangeActionMkdirAll creates a directory and any missing parents
	// (MkdirAll).
	ChangeActionMkdirAll ChangeAction = "mkdir_all"
	// ChangeActionRemove removes a single file or empty directory (Remove).
	ChangeActionRemove ChangeAction = "remove"
	// ChangeActionRemoveAll is an atomic whole-tree removal (RemoveAll).
	ChangeActionRemoveAll ChangeAction = "remove_all"
)

// ChangeSet is an immutable, serializable description of one atomic mutation —
// everything needed to commit it later via the applier, independent of what
// happens to the target in the meantime.
//
// It is the single primitive a consumer persists (e.g. while a write awaits
// human approval) and later replays with CommitChangeSet. It deliberately
// carries no approver / status / capability / proposer fields: those are the
// consumer's policy concern, not part of the content-addressed change.
//
// Action selects the payload: a write carries the exact proposed bytes plus its
// precondition contract; a remove_all carries the delete precondition. mkdir /
// mkdir_all / remove need only Target.
type ChangeSet struct {
	Target    string           `json:"target"`
	Action    ChangeAction     `json:"action"`
	Write     *WriteChange     `json:"write,omitempty"`
	RemoveAll *RemoveAllChange `json:"remove_all,omitempty"`
}

// WriteChange is the write payload of a ChangeSet: the exact proposed bytes plus
// the precondition contract they commit under. Opts is the caller's own
// WriteOpts, carried verbatim so a replay is byte-for-byte the write the caller
// requested — its zero value is an unconditional (last-write-wins) overwrite,
// an IfMatch pins a compare-and-swap base, and IfNoneMatch is create-only. This
// preserves every write mode through the log and through an approval hold.
type WriteChange struct {
	Bytes []byte    `json:"bytes"`
	Opts  WriteOpts `json:"opts"`
}

// RemoveAllChange is the whole-tree removal payload of a ChangeSet: the
// precondition contract the removal commits under, carried verbatim as the
// caller's own RemoveOpts. Its zero value (Expected nil) is an unconditional
// removal; a non-nil Expected snapshot pins the compare-and-swap base captured
// at proposal time.
type RemoveAllChange struct {
	Opts RemoveOpts `json:"opts"`
}

// CommitChangeSet replays cs against fs under the exact precondition contract it
// carries. A write commits its proposed bytes under its WriteOpts
// (unconditional / IfMatch / IfNoneMatch); a remove_all replays its whole-tree
// removal under its RemoveOpts; mkdir / mkdir_all / remove replay their
// namespace mutation. Compare-and-swap drift fails with *PreconditionError
// (write) or *TreeStaleError (remove_all) and commits nothing.
//
// It performs no authorization and captures no approver — the caller is
// responsible for deciding a ChangeSet may commit before calling this. For a
// write it returns the hex SHA-256 of the committed bytes; every other action
// returns an empty hash.
func CommitChangeSet(fs WritableFS, cs ChangeSet) (newHash string, err error) {
	switch cs.Action {
	case ChangeActionWrite:
		w := cs.Write
		if w == nil {
			return "", fmt.Errorf("changeset %s: missing write payload", cs.Target)
		}
		return fs.WriteFileAtomic(cs.Target, w.Bytes, w.Opts)
	case ChangeActionMkdir:
		return "", fs.Mkdir(cs.Target)
	case ChangeActionMkdirAll:
		return "", fs.MkdirAll(cs.Target)
	case ChangeActionRemove:
		return "", fs.Remove(cs.Target)
	case ChangeActionRemoveAll:
		r := cs.RemoveAll
		if r == nil {
			return "", fmt.Errorf("changeset %s: missing remove_all payload", cs.Target)
		}
		return "", fs.RemoveAll(cs.Target, r.Opts)
	default:
		return "", fmt.Errorf("changeset %s: unknown action %q", cs.Target, cs.Action)
	}
}

// PendingChangeError is returned by a mutating operation that an admission
// middleware intercepted and handed off instead of committing — for example,
// parked to await human approval. It is NOT a failure: the change was accepted
// as pending, so callers should report it informationally, not as an error.
//
// ChangeSet is the captured (immutable) mutation; Ref is the consumer-owned
// handle for the pending change (e.g. a held-changeset id) that callers surface
// so it can be resolved later.
type PendingChangeError struct {
	ChangeSet ChangeSet
	Ref       string
}

func (e *PendingChangeError) Error() string {
	if e.Ref != "" {
		return fmt.Sprintf("change to %s pending as %s", e.ChangeSet.Target, e.Ref)
	}
	return fmt.Sprintf("change to %s pending", e.ChangeSet.Target)
}
