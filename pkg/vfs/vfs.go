// Package vfs defines the virtual filesystem contract that the shell, its
// commands, and all backends share. It depends on nothing but the standard
// library: it is the seam between the shell (which operates on a filesystem)
// and the backends (which implement one).
package vfs

import (
	"fmt"
	"io/fs"
	"path"
	"time"
)

// FileInfo represents a file or directory in the virtual filesystem.
type FileInfo struct {
	FileName    string
	FilePath    string
	Content     []byte
	Dir         bool
	FileModTime time.Time
	FileSize    int64
}

func (fi *FileInfo) Name() string       { return fi.FileName }
func (fi *FileInfo) Size() int64        { return fi.FileSize }
func (fi *FileInfo) ModTime() time.Time { return fi.FileModTime }
func (fi *FileInfo) IsDir() bool        { return fi.Dir }
func (fi *FileInfo) Sys() interface{}   { return nil }

func (fi *FileInfo) Mode() fs.FileMode {
	if fi.Dir {
		return fs.ModeDir | 0555
	}
	return 0444
}

// FileSystem is the read-only filesystem interface that all commands operate on.
type FileSystem interface {
	Stat(path string) (*FileInfo, error)
	ReadDir(path string) ([]FileInfo, error)
	ReadFile(path string) ([]byte, error)
}

// WriteConflictPolicy selects how a whole-file overwrite (the `>`, tee, sed -i
// and publish verbs) defends against concurrent writers. It does not affect
// append (`>>`) or patch, which are always compare-and-swap by construction.
type WriteConflictPolicy string

const (
	// PolicyHash (the default) makes overwrites compare-and-swap: the write
	// carries an IfMatch precondition on the base the caller read, so a
	// concurrent change is rejected with a PreconditionError instead of being
	// silently clobbered. For a read-modify-write verb (sed -i) the base is the
	// content it transformed, giving true optimistic concurrency; for a blind
	// redirect the base is read at command time, so the guarantee narrows to
	// the command's own read→write window.
	PolicyHash WriteConflictPolicy = "hash"

	// PolicyLastWriteWins makes overwrites unconditional (the zero WriteOpts):
	// atomic, but the last writer silently wins. Opt-in per docset or globally.
	PolicyLastWriteWins WriteConflictPolicy = "last_write_wins"
)

// DefaultWriteConflictPolicy is the policy applied when none is configured.
const DefaultWriteConflictPolicy = PolicyHash

// ParseWriteConflictPolicy validates and normalizes a policy string. An empty
// string resolves to the default (hash). Unknown values are rejected.
func ParseWriteConflictPolicy(s string) (WriteConflictPolicy, error) {
	switch WriteConflictPolicy(s) {
	case "":
		return DefaultWriteConflictPolicy, nil
	case PolicyHash:
		return PolicyHash, nil
	case PolicyLastWriteWins:
		return PolicyLastWriteWins, nil
	default:
		return "", fmt.Errorf("invalid write_conflict_policy %q (want %q or %q)", s, PolicyHash, PolicyLastWriteWins)
	}
}

// WriteOpts carries the precondition contract for an atomic write. The zero
// value means "unconditional overwrite" (last-write-wins, but still atomic).
//
// Precedence: IfNoneMatch (create-only) is mutually exclusive with IfMatch.
type WriteOpts struct {
	// IfMatch, when non-nil, requires the current content hash of the target
	// to equal *IfMatch before the write commits. A nonexistent target fails
	// (you cannot match a hash that has no bytes). This is the hex SHA-256 of
	// the exact bytes ReadFile returns.
	IfMatch *string

	// IfNoneMatch, when true, requires the target to not already exist
	// (create-only). Fails with a conflict if it does.
	IfNoneMatch bool
}

// WritableFS is the read+write filesystem interface. A backend only satisfies
// it if it can physically persist writes; embedded/read-only backends
// (embed.FS, plain fs.FS adapters) deliberately do not implement it.
//
// Writes are whole-object and atomic by construction — there is no streaming,
// offset, or partial-write surface. The substrate's write capability is a
// stateful flag toggled by SetWriteable/SetReadonly; while read-only every
// mutating call returns an error.
type WritableFS interface {
	FileSystem

	// SetWriteable transitions the backend to writable. It returns an error
	// if the backend can't support writes at all (not if it is already
	// writable — that is an idempotent no-op success).
	SetWriteable() error

	// SetReadonly transitions the backend back to read-only. It is idempotent
	// and drains in-flight writes: a write that already passed its precondition
	// check is allowed to complete, and SetReadonly blocks until the substrate
	// is quiescent before returning.
	SetReadonly() error

	// WriteFileAtomic commits data to name as a single atomic whole-object
	// operation, honoring the WriteOpts precondition. It returns the hex
	// SHA-256 of the committed bytes. It errors when the backend is currently
	// read-only or when the precondition fails.
	WriteFileAtomic(name string, data []byte, opts WriteOpts) (newHash string, err error)

	// Mkdir creates a folder. It uses plain mkdir semantics (the parent must
	// exist) but errors if name is not strictly inside a docset — you cannot
	// create a docset, nor anything at or above a docset root, through the FS.
	Mkdir(name string) error
}

// WriteScopeFS is optionally implemented by a session filesystem that confines
// writes to a subset of paths. CanWrite reports whether a write to path would be
// permitted by the session's scope, without performing one — used for fail-fast
// checks (e.g. `spawn` validating its target before scheduling a job). It says
// nothing about content preconditions or approval gating, only scope.
type WriteScopeFS interface {
	CanWrite(path string) bool
}

// ErrReadOnly is returned by mutating operations when the substrate is in
// read-only mode.
var ErrReadOnly = fmt.Errorf("read-only filesystem")

// PreconditionError is returned when a WriteOpts precondition (IfMatch /
// IfNoneMatch) is not satisfied. Current is the hash of the existing bytes
// (empty if the target does not exist).
type PreconditionError struct {
	Path    string
	Current string
}

func (e *PreconditionError) Error() string {
	if e.Current == "" {
		return fmt.Sprintf("precondition failed: %s does not exist; re-read and retry", e.Path)
	}
	return fmt.Sprintf("precondition failed: %s current hash %s; re-read and retry", e.Path, e.Current)
}

// PendingApprovalError is returned by a mutating operation that was not
// committed because its target requires a human approval (Part C). The write
// has been recorded as a pending request (RequestID) that an approver holding
// Capability can approve or reject. It is not a failure — the proposal was
// accepted — so callers should report it informationally, not as an error.
type PendingApprovalError struct {
	RequestID  string
	Capability string
	Target     string
}

func (e *PendingApprovalError) Error() string {
	return fmt.Sprintf("write to %s pending approval as %s (requires %s)", e.Target, e.RequestID, e.Capability)
}

// WalkDir walks the filesystem tree rooted at root, calling fn for each file or directory.
func WalkDir(fsys FileSystem, root string, fn func(path string, info *FileInfo, err error) error) error {
	root = CleanPath(root)
	f, err := fsys.Stat(root)
	if err != nil {
		return fn(root, nil, err)
	}
	if err := fn(root, f, nil); err != nil {
		return err
	}
	if !f.Dir {
		return nil
	}
	entries, err := fsys.ReadDir(root)
	if err != nil {
		return fn(root, nil, err)
	}
	for _, entry := range entries {
		childPath := path.Join(root, entry.FileName)
		if err := WalkDir(fsys, childPath, fn); err != nil {
			return err
		}
	}
	return nil
}

// CleanPath normalises a path to always start with / and removes . and .. segments.
func CleanPath(p string) string {
	p = path.Clean("/" + p)
	if p == "." {
		p = "/"
	}
	return p
}

// Dir is a convenience constructor for a directory FileInfo.
func Dir(p string, modTime time.Time) *FileInfo {
	return &FileInfo{
		FileName:    path.Base(p),
		FilePath:    p,
		Dir:         true,
		FileModTime: modTime,
	}
}

// File is a convenience constructor for a file FileInfo.
func File(name, filePath string, content []byte, modTime time.Time) *FileInfo {
	return &FileInfo{
		FileName:    name,
		FilePath:    filePath,
		Content:     content,
		Dir:         false,
		FileModTime: modTime,
		FileSize:    int64(len(content)),
	}
}

// ErrNotFound is returned when a path does not exist.
func ErrNotFound(p string) error {
	return fmt.Errorf("not found: %s", p)
}

// ErrIsDirectory is returned when a file operation is attempted on a directory.
func ErrIsDirectory(p string) error {
	return fmt.Errorf("is a directory: %s", p)
}

// ErrNotDirectory is returned when a directory operation is attempted on a file.
func ErrNotDirectory(p string) error {
	return fmt.Errorf("not a directory: %s", p)
}
