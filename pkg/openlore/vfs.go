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

	"github.com/aakarim/go-openlore/internal/config"
	"github.com/aakarim/go-openlore/pkg/openlore/eventbus"
	"github.com/aakarim/go-openlore/pkg/vfs"
)

// DirFS serves files from a real directory on disk.
//
// DirFS is read-only by default. Call WithBus(...) to obtain a copy that
// supports WriteFile and emits a KindPostWrite event on each successful
// write. (P1-06.)
type DirFS struct {
	root  string
	files config.FilesConfig
	bus   *eventbus.Bus // optional; nil means writes are rejected
}

// NewDirFS creates a new DirFS rooted at the given directory.
func NewDirFS(root string, files config.FilesConfig) *DirFS {
	return &DirFS{root: root, files: files}
}

// WithBus returns a copy of DirFS that allows writes via WriteFile and fans
// every successful write out as a KindPostWrite event on the supplied bus.
// Pass nil to disable writes.
func (d *DirFS) WithBus(bus *eventbus.Bus) *DirFS {
	c := *d
	c.bus = bus
	return &c
}

// WriteFile writes content to the path inside the DirFS root. Errors if the
// DirFS was constructed without a bus (read-only mode). On success, emits a
// KindPostWrite event so subscribers (DB writer, SSE fanout, Notifier,
// post_write shell hooks) can react.
func (d *DirFS) WriteFile(p string, content []byte) error {
	if d.bus == nil {
		return fmt.Errorf("read-only: DirFS has no event bus configured")
	}
	if !isAllowed(path.Base(p), d.files) {
		return fmt.Errorf("access denied: %s", p)
	}
	if isIgnored(p, d.files) {
		return fmt.Errorf("access denied: %s", p)
	}
	full := d.resolve(p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	_ = d.bus.Publish(context.Background(), eventbus.Event{
		Kind:        eventbus.KindPostWrite,
		Path:        vfs.CleanPath(p),
		ContentHash: hash,
		Bytes:       len(content),
	})
	return nil
}

func (d *DirFS) resolve(p string) string {
	p = path.Clean("/" + p)
	return filepath.Join(d.root, filepath.FromSlash(p))
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

// Ensure all types implement vfs.FileSystem.
var (
	_ vfs.FileSystem = (*DirFS)(nil)
	_ vfs.FileSystem = (*MergeFS)(nil)
	_ vfs.FileSystem = (*EmbedFS)(nil)
	_ vfs.FileSystem = (*FSAdapter)(nil)
)
