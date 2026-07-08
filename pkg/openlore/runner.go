package openlore

import (
	"context"
	"os/exec"
)

// Runner executes a shell command line. Production uses ShellRunner; tests
// substitute a fake. It backs the built-in shellexec plugin and the async
// job manager (spawn).
type Runner interface {
	// Run executes cmd with the given env. Returns the command's combined
	// output (stdout+stderr) and any execution error. Honour ctx for
	// cancellation/timeout.
	Run(ctx context.Context, cmd string, env []string) ([]byte, error)
}

// ShellRunner runs commands via `sh -c`.
type ShellRunner struct{}

func (ShellRunner) Run(ctx context.Context, cmdLine string, env []string) ([]byte, error) {
	c := exec.CommandContext(ctx, "sh", "-c", cmdLine)
	c.Env = env
	return c.CombinedOutput()
}
