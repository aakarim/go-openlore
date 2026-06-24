package cmds

import (
	"io"
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

// CmdContext provides the interface that commands use to interact with the shell.
// Implemented by shell.Shell.
type CmdContext interface {
	FS() FileSystem
	Cwd() string
	SetCwd(dir string)
	Resolve(path string) string
	GetEnv(key string) string
	SetEnv(key, value string)
	DeleteEnv(key string)
	AllEnv() map[string]string
	Exec(cmdLine string, w io.Writer, errW io.Writer, stdin io.Reader) int
	ExecPipeline(line string, w io.Writer, errW io.Writer, stdin io.Reader) int
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
