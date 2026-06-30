// Package hooks implements the storage substrate hook subscribers (P1-04
// and P1-05). They run external shell commands in response to bus events.
//
// Configuration is loaded from openlore.yml. Defaults: non-fatal hook errors.
// Set fail_on_error: true on a hook to flip to fatal.
package hooks

import (
	"errors"
	"fmt"
	"time"
)

// Config is the openlore.yml hooks block.
//
// Example:
//
//	data_dir: /var/lib/openlore
//	hooks:
//	  on_startup:
//	    - cmd: "/usr/local/bin/openlore-restore"
//	      timeout: 30s
//	  pre_read:
//	    - cmd: "/usr/local/bin/openlore-pull"
//	      debounce: 2s
//	  post_write:
//	    - cmd: "/usr/local/bin/openlore-version"
//	      fail_on_error: true
type Config struct {
	// DataDir is the disk root passed to hooks via OPENLORE_DATA_DIR.
	DataDir string `yaml:"data_dir"`
	// Hooks is the per-event command list.
	Hooks HookSet `yaml:"hooks"`
}

// HookSet groups commands by event kind.
type HookSet struct {
	OnStartup       []HookCmd `yaml:"on_startup"`
	PreRead         []HookCmd `yaml:"pre_read"`
	PostWrite       []HookCmd `yaml:"post_write"`
	ApprovalPending []HookCmd `yaml:"approval_pending"`
}

// HookCmd is a single shell command to run on an event.
type HookCmd struct {
	// Cmd is the shell command line to execute. Run via `sh -c`.
	Cmd string `yaml:"cmd"`
	// Timeout caps the command's wall-clock runtime. Zero means
	// DefaultHookTimeout.
	Timeout time.Duration `yaml:"timeout"`
	// FailOnError makes a non-zero exit fatal to the publisher. Defaults to
	// false (errors logged, never propagated).
	FailOnError bool `yaml:"fail_on_error"`
	// Debounce coalesces repeated pre_read hits on the same path. Zero means
	// DefaultDebounce. Only applies to pre_read hooks; ignored elsewhere.
	Debounce time.Duration `yaml:"debounce"`
}

// DefaultHookTimeout is the per-hook wall-clock cap if Timeout is unset.
const DefaultHookTimeout = 30 * time.Second

// DefaultDebounce is the pre_read debounce window if Debounce is unset.
const DefaultDebounce = 2 * time.Second

// Validate checks the config for shape errors. Empty configs are valid (no
// hooks fire).
func (c *Config) Validate() error {
	var errs []error
	for _, h := range c.Hooks.OnStartup {
		if err := h.validate("on_startup"); err != nil {
			errs = append(errs, err)
		}
	}
	for _, h := range c.Hooks.PreRead {
		if err := h.validate("pre_read"); err != nil {
			errs = append(errs, err)
		}
	}
	for _, h := range c.Hooks.PostWrite {
		if err := h.validate("post_write"); err != nil {
			errs = append(errs, err)
		}
	}
	for _, h := range c.Hooks.ApprovalPending {
		if err := h.validate("approval_pending"); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (h HookCmd) validate(kind string) error {
	if h.Cmd == "" {
		return fmt.Errorf("%s hook: cmd is required", kind)
	}
	if h.Timeout < 0 {
		return fmt.Errorf("%s hook %q: timeout must be non-negative", kind, h.Cmd)
	}
	if h.Debounce < 0 {
		return fmt.Errorf("%s hook %q: debounce must be non-negative", kind, h.Cmd)
	}
	return nil
}

// timeout returns the effective timeout, applying DefaultHookTimeout if zero.
func (h HookCmd) timeout() time.Duration {
	if h.Timeout <= 0 {
		return DefaultHookTimeout
	}
	return h.Timeout
}

// debounce returns the effective debounce, applying DefaultDebounce if zero.
func (h HookCmd) debounce() time.Duration {
	if h.Debounce <= 0 {
		return DefaultDebounce
	}
	return h.Debounce
}
