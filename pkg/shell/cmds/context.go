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
	// ActionAllowed reports whether the session may perform the given
	// capability class. A command that introspects the available surface
	// (e.g. help) uses this to hide actions the session is not allowed to use.
	ActionAllowed(a Action) bool
	// WriteConflictPolicy reports the policy governing whole-file overwrites to
	// the given resolved path. Defaults to vfs.PolicyHash (compare-and-swap)
	// when the host has not configured a resolver.
	WriteConflictPolicy(resolvedPath string) vfs.WriteConflictPolicy
}
