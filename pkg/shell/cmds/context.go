package cmds

import (
	"io"

	"github.com/aakarim/go-openlore/pkg/vfs"
)

// CmdContext provides the interface that commands use to interact with the shell.
// Implemented by shell.Shell.
type CmdContext interface {
	FS() vfs.FileSystem
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
