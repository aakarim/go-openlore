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
