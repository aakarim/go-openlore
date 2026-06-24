package shell

import (
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/aakarim/go-openlore/pkg/shell/cmds"
)

// FileSystem is the interface that the shell operates on.
type FileSystem = cmds.FileSystem

// FileInfo represents a file or directory in the virtual filesystem.
type FileInfo = cmds.FileInfo

// CmdContext is the interface commands use to interact with the shell.
type CmdContext = cmds.CmdContext

// fs.FileInfo interface implementation — these are extension methods
// that FileInfo satisfies for Go's fs package.

func fileInfoSize(fi *FileInfo) int64        { return fi.FileSize }
func fileInfoModTime(fi *FileInfo) time.Time { return fi.FileModTime }
func fileInfoIsDir(fi *FileInfo) bool        { return fi.Dir }
func fileInfoSys(fi *FileInfo) interface{}   { return nil }

func fileInfoMode(fi *FileInfo) fs.FileMode {
	if fi.Dir {
		return fs.ModeDir | 0555
	}
	return 0444
}

// WalkDir walks the filesystem tree rooted at root, calling fn for each file or directory.
var WalkDir = cmds.WalkDir

// CleanPath normalises a path to always start with / and removes . and .. segments.
var CleanPath = cmds.CleanPath

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
