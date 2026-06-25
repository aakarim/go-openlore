package openlore

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/openlore/eventbus"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// DirFS serves files from a real directory on disk. It is the reference
// vfs.WritableFS implementation.
//
// Write capability is a stateful flag (the substrate-wide readonly lock). A
// freshly constructed DirFS is read-only; call SetWriteable to enable writes.
// WriteFileAtomic commits whole objects via temp-file + fsync + rename(2)
// (POSIX atomic swap), and emits a KindPostWrite event when a bus is set
// (see WithBus).
type DirFS struct {
	root  string
	files config.FilesConfig
	bus   *eventbus.Bus // optional; nil means no post_write fanout

	// docsetRoots are the logical paths (relative to this DirFS root) that are
	// docset boundaries for Mkdir: a folder may only be created strictly below
	// one of them. Empty means the whole DirFS is treated as a single docset
	// (any non-root path is allowed).
	docsetRoots []string

	// stateMu guards the writeable flag and drains in-flight writes. Writers
	// take RLock for the duration of a mutation; SetReadonly takes the
	// exclusive Lock, which blocks until in-flight writers release (drain).
	stateMu   sync.RWMutex
	writeable bool

	// commitMu serializes the precondition check-and-swap so concurrent writers
	// to the same DirFS never interleave their read-current → check → rename.
	commitMu sync.Mutex

	// maxWriteBytes caps a single atomic write; 0 means use the default.
	maxWriteBytes int64
}

// defaultMaxWriteBytes bounds a single buffered atomic write (knowledge
// objects are small).
const defaultMaxWriteBytes = 8 << 20 // 8 MiB

// NewDirFS creates a new (read-only) DirFS rooted at the given directory.
func NewDirFS(root string, files config.FilesConfig) *DirFS {
	return &DirFS{root: root, files: files}
}

// WithBus sets the event bus that receives a KindPostWrite event after every
// successful write, and returns the receiver for chaining. Pass nil to disable
// fanout. Configure before the DirFS is shared across goroutines.
func (d *DirFS) WithBus(bus *eventbus.Bus) *DirFS {
	d.bus = bus
	return d
}

// WithDocsetRoots sets the Mkdir boundary to the given logical docset roots — a
// folder may only be created strictly below one of them — and returns the
// receiver for chaining. Configure before the DirFS is shared across
// goroutines.
func (d *DirFS) WithDocsetRoots(roots []string) *DirFS {
	cleaned := make([]string, 0, len(roots))
	for _, r := range roots {
		cleaned = append(cleaned, vfs.CleanPath(r))
	}
	d.docsetRoots = cleaned
	return d
}

// SetWriteable transitions the substrate to writable. Idempotent.
func (d *DirFS) SetWriteable() error {
	d.stateMu.Lock()
	d.writeable = true
	d.stateMu.Unlock()
	return nil
}

// SetReadonly transitions the substrate back to read-only, draining in-flight
// writes first (the exclusive lock blocks until current writers release).
// Idempotent.
func (d *DirFS) SetReadonly() error {
	d.stateMu.Lock()
	d.writeable = false
	d.stateMu.Unlock()
	return nil
}

// WriteFileAtomic commits content to p as a single atomic object. The
// precondition (opts) is checked under the same lock that guards the commit, so
// the read-current → check → swap sequence is atomic. Returns the hex SHA-256
// of the committed bytes.
func (d *DirFS) WriteFileAtomic(p string, content []byte, opts vfs.WriteOpts) (string, error) {
	max := d.maxWriteBytes
	if max == 0 {
		max = defaultMaxWriteBytes
	}
	if int64(len(content)) > max {
		return "", fmt.Errorf("write rejected: %d bytes exceeds limit of %d", len(content), max)
	}
	if !isAllowed(path.Base(p), d.files) {
		return "", fmt.Errorf("access denied: %s", p)
	}
	if isIgnored(p, d.files) {
		return "", fmt.Errorf("access denied: %s", p)
	}

	// Hold RLock for the whole mutation: it permits concurrent writers but
	// blocks SetReadonly from completing until we release (drain semantics).
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	if !d.writeable {
		return "", vfs.ErrReadOnly
	}

	full := d.resolve(p)

	// Precondition check + commit must be atomic with respect to other writers
	// to the same DirFS. A single mutex serializes the check-and-swap.
	d.commitMu.Lock()
	defer d.commitMu.Unlock()

	if opts.IfMatch != nil || opts.IfNoneMatch {
		cur, exists, err := currentHash(full)
		if err != nil {
			return "", err
		}
		if opts.IfNoneMatch && exists {
			return "", &vfs.PreconditionError{Path: vfs.CleanPath(p), Current: cur}
		}
		if opts.IfMatch != nil {
			if !exists || cur != *opts.IfMatch {
				return "", &vfs.PreconditionError{Path: vfs.CleanPath(p), Current: cur}
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	if err := atomicWrite(full, content); err != nil {
		return "", err
	}

	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	if d.bus != nil {
		_ = d.bus.Publish(context.Background(), eventbus.Event{
			Kind:        eventbus.KindPostWrite,
			Path:        vfs.CleanPath(p),
			ContentHash: hash,
			Bytes:       len(content),
		})
	}
	return hash, nil
}

// Mkdir creates a folder at p using plain mkdir semantics (the parent must
// exist). It errors if p is not strictly below a docset root.
func (d *DirFS) Mkdir(p string) error {
	clean := vfs.CleanPath(p)
	if clean == "/" {
		return fmt.Errorf("cannot create docset root: %s", p)
	}
	if isIgnored(p, d.files) {
		return fmt.Errorf("access denied: %s", p)
	}
	if !d.insideDocset(clean) {
		return fmt.Errorf("cannot create folder outside a docset: %s", p)
	}

	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	if !d.writeable {
		return vfs.ErrReadOnly
	}

	full := d.resolve(p)
	if err := os.Mkdir(full, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return nil
}

// insideDocset reports whether the cleaned logical path sits strictly below a
// docset root. With no docset roots configured, the whole DirFS is one docset,
// so any non-root path qualifies.
func (d *DirFS) insideDocset(clean string) bool {
	if len(d.docsetRoots) == 0 {
		return clean != "/"
	}
	for _, root := range d.docsetRoots {
		if root == "/" {
			if clean != "/" {
				return true
			}
			continue
		}
		if strings.HasPrefix(clean, root+"/") {
			return true
		}
	}
	return false
}

func (d *DirFS) resolve(p string) string {
	p = path.Clean("/" + p)
	return filepath.Join(d.root, filepath.FromSlash(p))
}

// currentHash returns the hex SHA-256 of the bytes currently at full, and
// whether the file exists. A directory is treated as nonexistent for hashing.
func currentHash(full string) (hash string, exists bool, err error) {
	data, err := os.ReadFile(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		// A directory read returns an error; treat as "exists, no hash".
		if info, statErr := os.Stat(full); statErr == nil && info.IsDir() {
			return "", true, nil
		}
		return "", false, err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), true, nil
}

// atomicWrite writes content to a temp file in the destination directory,
// fsyncs it, then atomically renames it into place (POSIX atomic swap).
func atomicWrite(full string, content []byte) error {
	dir := filepath.Dir(full)
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(full)+"-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, full); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func (d *DirFS) Stat(p string) (*vfs.FileInfo, error) {
	full := d.resolve(p)
	info, err := os.Stat(full)
	if err != nil {
		return nil, err
	}

	if !info.IsDir() && !isAllowed(info.Name(), d.files) {
		return nil, fmt.Errorf("access denied: %s", p)
	}

	if info.IsDir() && isIgnored(p, d.files) {
		return nil, fmt.Errorf("access denied: %s", p)
	}

	return &vfs.FileInfo{
		FileName:    info.Name(),
		FilePath:    vfs.CleanPath(p),
		FileSize:    info.Size(),
		FileModTime: info.ModTime(),
		Dir:         info.IsDir(),
	}, nil
}

func (d *DirFS) ReadDir(p string) ([]vfs.FileInfo, error) {
	if isIgnored(p, d.files) {
		return nil, fmt.Errorf("access denied: %s", p)
	}

	full := d.resolve(p)
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}

	var result []vfs.FileInfo
	for _, e := range entries {
		childPath := path.Join(p, e.Name())
		if e.IsDir() {
			if isIgnored(childPath, d.files) {
				continue
			}
		} else {
			if !isAllowed(e.Name(), d.files) {
				continue
			}
		}

		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, vfs.FileInfo{
			FileName:    e.Name(),
			FilePath:    vfs.CleanPath(childPath),
			FileSize:    info.Size(),
			FileModTime: info.ModTime(),
			Dir:         e.IsDir(),
		})
	}
	return result, nil
}

func (d *DirFS) ReadFile(p string) ([]byte, error) {
	if !isAllowed(path.Base(p), d.files) {
		return nil, fmt.Errorf("access denied: %s", p)
	}

	full := d.resolve(p)
	return os.ReadFile(full)
}

// MergeFS merges multiple filesystems under named mount points.
// An optional root filesystem serves content directly at "/".
type MergeFS struct {
	root   vfs.FileSystem
	mounts map[string]vfs.FileSystem
}

// NewMergeFS creates an empty MergeFS.
func NewMergeFS() *MergeFS {
	return &MergeFS{mounts: make(map[string]vfs.FileSystem)}
}

// SetRoot sets the root filesystem that serves content at "/".
func (m *MergeFS) SetRoot(fs vfs.FileSystem) {
	m.root = fs
}

// Mount adds a filesystem under the given name.
func (m *MergeFS) Mount(name string, fs vfs.FileSystem) {
	m.mounts[name] = fs
}

// FilteredView returns a new MergeFS that only includes the specified mount names.
// The root filesystem is always included. If allowedMounts is nil, returns the original.
func (m *MergeFS) FilteredView(allowedMounts map[string]bool) *MergeFS {
	if allowedMounts == nil {
		return m
	}
	filtered := &MergeFS{
		root:   m.root,
		mounts: make(map[string]vfs.FileSystem),
	}
	for name, fs := range m.mounts {
		if allowedMounts[name] {
			filtered.mounts[name] = fs
		}
	}
	return filtered
}

// SetWriteable fans out to every writable-capable backend (root + mounts).
// Read-only backends (EmbedFS, FSAdapter) are skipped. It fails fast if no
// backend can be made writable at all (e.g. a fully embedded, read-only
// distribution), so a misconfigured readonly=false is rejected at startup.
func (m *MergeFS) SetWriteable() error {
	var enabled int
	if w, ok := m.root.(vfs.WritableFS); ok {
		if err := w.SetWriteable(); err != nil {
			return err
		}
		enabled++
	}
	for _, fsys := range m.mounts {
		if w, ok := fsys.(vfs.WritableFS); ok {
			if err := w.SetWriteable(); err != nil {
				return err
			}
			enabled++
		}
	}
	if enabled == 0 {
		return fmt.Errorf("%w: no writable backend (cannot enable writes)", vfs.ErrReadOnly)
	}
	return nil
}

// SetReadonly fans out to every writable-capable backend, draining in-flight
// writes on each.
func (m *MergeFS) SetReadonly() error {
	return m.fanout(func(w vfs.WritableFS) error { return w.SetReadonly() })
}

func (m *MergeFS) fanout(fn func(vfs.WritableFS) error) error {
	if w, ok := m.root.(vfs.WritableFS); ok {
		if err := fn(w); err != nil {
			return err
		}
	}
	for _, fsys := range m.mounts {
		if w, ok := fsys.(vfs.WritableFS); ok {
			if err := fn(w); err != nil {
				return err
			}
		}
	}
	return nil
}

// WriteFileAtomic routes the write to the resolved mount (or root). It errors
// if the path resolves to the merge root itself or to a read-only backend.
func (m *MergeFS) WriteFileAtomic(p string, content []byte, opts vfs.WriteOpts) (string, error) {
	subPath, fsys, err := m.resolve(p)
	if err != nil {
		return "", err
	}
	if fsys == nil {
		return "", fmt.Errorf("cannot write to filesystem root: %s", p)
	}
	w, ok := fsys.(vfs.WritableFS)
	if !ok {
		return "", fmt.Errorf("%w: %s", vfs.ErrReadOnly, p)
	}
	return w.WriteFileAtomic(subPath, content, opts)
}

// Mkdir routes the folder creation to the resolved mount. Creating a docset
// (the merge root, or a mount root) is not allowed.
func (m *MergeFS) Mkdir(p string) error {
	subPath, fsys, err := m.resolve(p)
	if err != nil {
		return err
	}
	if fsys == nil {
		return fmt.Errorf("cannot create docset at filesystem root: %s", p)
	}
	if vfs.CleanPath(subPath) == "/" {
		return fmt.Errorf("cannot create docset root: %s", p)
	}
	w, ok := fsys.(vfs.WritableFS)
	if !ok {
		return fmt.Errorf("%w: %s", vfs.ErrReadOnly, p)
	}
	return w.Mkdir(subPath)
}

func (m *MergeFS) resolve(p string) (string, vfs.FileSystem, error) {
	p = path.Clean("/" + p)
	p = strings.TrimPrefix(p, "/")

	if p == "" || p == "." {
		return "", nil, nil // root listing
	}

	// Check named mounts first
	parts := strings.SplitN(p, "/", 2)
	mountName := parts[0]

	if fsys, ok := m.mounts[mountName]; ok {
		subPath := "/"
		if len(parts) > 1 {
			subPath = "/" + parts[1]
		}
		return subPath, fsys, nil
	}

	// Fall back to root filesystem
	if m.root != nil {
		return "/" + p, m.root, nil
	}

	return "", nil, fmt.Errorf("not found: %s", p)
}

func (m *MergeFS) Stat(p string) (*vfs.FileInfo, error) {
	subPath, fsys, err := m.resolve(p)
	if err != nil {
		return nil, err
	}

	// Root directory
	if fsys == nil {
		return &vfs.FileInfo{
			FileName: "/",
			FilePath: "/",
			Dir:      true,
		}, nil
	}

	return fsys.Stat(subPath)
}

func (m *MergeFS) ReadDir(p string) ([]vfs.FileInfo, error) {
	subPath, fsys, err := m.resolve(p)
	if err != nil {
		return nil, err
	}

	// Root: merge root FS entries with mount points
	if fsys == nil {
		var entries []vfs.FileInfo
		if m.root != nil {
			rootEntries, err := m.root.ReadDir("/")
			if err == nil {
				entries = append(entries, rootEntries...)
			}
		}
		for name := range m.mounts {
			entries = append(entries, vfs.FileInfo{
				FileName: name,
				FilePath: "/" + name,
				Dir:      true,
			})
		}
		return entries, nil
	}

	return fsys.ReadDir(subPath)
}

func (m *MergeFS) ReadFile(p string) ([]byte, error) {
	subPath, fsys, err := m.resolve(p)
	if err != nil {
		return nil, err
	}

	if fsys == nil {
		return nil, fmt.Errorf("cannot read directory")
	}

	return fsys.ReadFile(subPath)
}

// EmbedFS serves files from an embed.FS.
type EmbedFS struct {
	fs    embed.FS
	root  string
	files config.FilesConfig
}

// NewEmbedFS creates a new EmbedFS.
func NewEmbedFS(efs embed.FS, root string, files config.FilesConfig) *EmbedFS {
	return &EmbedFS{fs: efs, root: root, files: files}
}

func (e *EmbedFS) resolve(p string) string {
	p = path.Clean("/" + p)
	p = strings.TrimPrefix(p, "/")
	if p == "" || p == "." {
		return e.root
	}
	return path.Join(e.root, p)
}

func (e *EmbedFS) Stat(p string) (*vfs.FileInfo, error) {
	full := e.resolve(p)
	f, err := e.fs.Open(full)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if !info.IsDir() && !isAllowed(info.Name(), e.files) {
		return nil, fmt.Errorf("access denied: %s", p)
	}

	return &vfs.FileInfo{
		FileName:    info.Name(),
		FilePath:    vfs.CleanPath(p),
		FileSize:    info.Size(),
		FileModTime: info.ModTime(),
		Dir:         info.IsDir(),
	}, nil
}

func (e *EmbedFS) ReadDir(p string) ([]vfs.FileInfo, error) {
	full := e.resolve(p)
	entries, err := e.fs.ReadDir(full)
	if err != nil {
		return nil, err
	}

	var result []vfs.FileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			childPath := path.Join(p, entry.Name())
			if isIgnored(childPath, e.files) {
				continue
			}
		} else {
			if !isAllowed(entry.Name(), e.files) {
				continue
			}
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}
		result = append(result, vfs.FileInfo{
			FileName:    entry.Name(),
			FilePath:    vfs.CleanPath(path.Join(p, entry.Name())),
			FileSize:    info.Size(),
			FileModTime: info.ModTime(),
			Dir:         entry.IsDir(),
		})
	}
	return result, nil
}

func (e *EmbedFS) ReadFile(p string) ([]byte, error) {
	if !isAllowed(path.Base(p), e.files) {
		return nil, fmt.Errorf("access denied: %s", p)
	}

	full := e.resolve(p)
	return e.fs.ReadFile(full)
}

// isAllowed checks if a filename matches the allowed patterns (and is not denied).
// If no allowed patterns are configured, all files are allowed.
func isAllowed(name string, files config.FilesConfig) bool {
	// Check denied first
	for _, pattern := range files.Denied {
		if matched, _ := path.Match(pattern, name); matched {
			return false
		}
	}

	// If no allowed patterns, everything is allowed
	if len(files.Allowed) == 0 {
		return true
	}

	for _, pattern := range files.Allowed {
		if matched, _ := path.Match(pattern, name); matched {
			return true
		}
	}

	return false
}

// isIgnored checks if a path matches any ignore patterns.
func isIgnored(p string, files config.FilesConfig) bool {
	p = path.Clean("/" + p)
	p = strings.TrimPrefix(p, "/")

	for _, pattern := range files.Ignore {
		if matched, _ := path.Match(pattern, p); matched {
			return true
		}
		if matched, _ := path.Match(pattern, path.Base(p)); matched {
			return true
		}
		parts := strings.Split(p, "/")
		for i := range parts {
			segment := strings.Join(parts[:i+1], "/")
			if matched, _ := path.Match(pattern, segment); matched {
				return true
			}
		}
	}
	return false
}

// FSAdapter adapts a standard fs.FS to the vfs.FileSystem interface.
type FSAdapter struct {
	fsys fs.FS
}

// NewFSAdapter creates a new FSAdapter.
func NewFSAdapter(fsys fs.FS) *FSAdapter {
	return &FSAdapter{fsys: fsys}
}

func (a *FSAdapter) Stat(p string) (*vfs.FileInfo, error) {
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" {
		p = "."
	}
	info, err := fs.Stat(a.fsys, p)
	if err != nil {
		return nil, err
	}
	return &vfs.FileInfo{
		FileName:    info.Name(),
		FilePath:    vfs.CleanPath("/" + p),
		FileSize:    info.Size(),
		FileModTime: info.ModTime(),
		Dir:         info.IsDir(),
	}, nil
}

func (a *FSAdapter) ReadDir(p string) ([]vfs.FileInfo, error) {
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" {
		p = "."
	}
	entries, err := fs.ReadDir(a.fsys, p)
	if err != nil {
		return nil, err
	}
	var result []vfs.FileInfo
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, vfs.FileInfo{
			FileName:    e.Name(),
			FilePath:    vfs.CleanPath("/" + path.Join(p, e.Name())),
			FileSize:    info.Size(),
			FileModTime: info.ModTime(),
			Dir:         e.IsDir(),
		})
	}
	return result, nil
}

func (a *FSAdapter) ReadFile(p string) ([]byte, error) {
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" {
		p = "."
	}
	return fs.ReadFile(a.fsys, p)
}

// Ensure all types implement vfs.FileSystem; DirFS and MergeFS are writable.
// EmbedFS and FSAdapter are deliberately read-only (no WritableFS).
var (
	_ vfs.FileSystem = (*DirFS)(nil)
	_ vfs.FileSystem = (*MergeFS)(nil)
	_ vfs.FileSystem = (*EmbedFS)(nil)
	_ vfs.FileSystem = (*FSAdapter)(nil)
	_ vfs.WritableFS = (*DirFS)(nil)
	_ vfs.WritableFS = (*MergeFS)(nil)
)
