package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/aakarim/go-openlore/pkg/openlore/eventbus"
)

// Runner executes a shell command line. Production uses ShellRunner; tests
// substitute a fake.
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

// Subscriber wraps a single HookCmd as an eventbus.Subscriber.
//
// Env-var protocol passed to the child process:
//   - OPENLORE_DATA_DIR  — server data root
//   - OPENLORE_PATH      — virtual path the event refers to
//   - OPENLORE_AGENT     — publishing agent ID (or empty)
//   - OPENLORE_BYTES     — byte count (post_write only)
//   - OPENLORE_EVENT     — event kind (on_startup / pre_read / post_write)
//   - OPENLORE_PARTITION — partition slug (or empty)
type Subscriber struct {
	name    string
	kind    eventbus.EventKind
	cmd     HookCmd
	dataDir string
	runner  Runner
	logger  *slog.Logger

	// debounce state for pre_read.
	debouncerMu sync.Mutex
	lastFired   map[string]time.Time
}

// NewSubscriber wraps a HookCmd for a specific event kind.
func NewSubscriber(name string, kind eventbus.EventKind, cmd HookCmd, dataDir string, runner Runner, logger *slog.Logger) *Subscriber {
	if runner == nil {
		runner = ShellRunner{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Subscriber{
		name:      name,
		kind:      kind,
		cmd:       cmd,
		dataDir:   dataDir,
		runner:    runner,
		logger:    logger,
		lastFired: map[string]time.Time{},
	}
}

func (s *Subscriber) Name() string   { return s.name }
func (s *Subscriber) Required() bool { return s.cmd.FailOnError }

// Handle runs the configured shell command for the event, if the event kind
// matches. Pre-read events are debounced per-path. Honours the per-hook
// timeout. Errors are returned for the bus to decide fatality.
func (s *Subscriber) Handle(ctx context.Context, e eventbus.Event) error {
	if e.Kind != s.kind {
		return nil
	}
	if s.kind == eventbus.KindPreRead && s.shouldDebounce(e.Path) {
		return nil
	}

	timeout := s.cmd.timeout()
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	env := s.buildEnv(e)
	out, err := s.runner.Run(runCtx, s.cmd.Cmd, env)
	if err != nil {
		s.logger.Warn("hook command failed",
			"hook", s.name,
			"event", e.Kind,
			"path", e.Path,
			"err", err,
			"output", truncate(out, 512),
		)
		return fmt.Errorf("hook %q (%s): %w", s.name, e.Kind, err)
	}
	if len(out) > 0 {
		s.logger.Debug("hook command output",
			"hook", s.name,
			"event", e.Kind,
			"path", e.Path,
			"output", truncate(out, 512),
		)
	}
	return nil
}

// shouldDebounce returns true if a pre_read for this path fired within the
// debounce window. Records the fire-time as a side effect when not debounced.
func (s *Subscriber) shouldDebounce(path string) bool {
	window := s.cmd.debounce()
	now := time.Now()

	s.debouncerMu.Lock()
	defer s.debouncerMu.Unlock()
	if last, ok := s.lastFired[path]; ok && now.Sub(last) < window {
		return true
	}
	s.lastFired[path] = now
	return false
}

func (s *Subscriber) buildEnv(e eventbus.Event) []string {
	env := []string{
		"OPENLORE_DATA_DIR=" + s.dataDir,
		"OPENLORE_PATH=" + e.Path,
		"OPENLORE_AGENT=" + e.Agent,
		"OPENLORE_PARTITION=" + e.Partition,
		"OPENLORE_EVENT=" + string(e.Kind),
	}
	if e.Kind == eventbus.KindPostWrite {
		env = append(env, "OPENLORE_BYTES="+strconv.Itoa(e.Bytes))
	}
	for k, v := range e.Extra {
		env = append(env, "OPENLORE_EXTRA_"+k+"="+v)
	}
	return env
}

// Subscribe wires every HookCmd in cfg as an eventbus.Subscriber on bus. It
// returns the list of registered Subscribers (mostly for tests).
func Subscribe(bus *eventbus.Bus, cfg Config, runner Runner, logger *slog.Logger) []*Subscriber {
	var out []*Subscriber
	for i, h := range cfg.Hooks.OnStartup {
		s := NewSubscriber(fmt.Sprintf("on_startup[%d]", i), eventbus.KindOnStartup, h, cfg.DataDir, runner, logger)
		bus.Subscribe(s)
		out = append(out, s)
	}
	for i, h := range cfg.Hooks.PreRead {
		s := NewSubscriber(fmt.Sprintf("pre_read[%d]", i), eventbus.KindPreRead, h, cfg.DataDir, runner, logger)
		bus.Subscribe(s)
		out = append(out, s)
	}
	for i, h := range cfg.Hooks.PostWrite {
		s := NewSubscriber(fmt.Sprintf("post_write[%d]", i), eventbus.KindPostWrite, h, cfg.DataDir, runner, logger)
		bus.Subscribe(s)
		out = append(out, s)
	}
	return out
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "...(truncated)"
}
