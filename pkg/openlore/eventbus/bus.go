// Package eventbus is the openlore storage-event bus. It is the single point
// where write events (kb publish, admin write, processor result, …) fan out
// to all subscribers — DB writer, SSE fanout, Notifier, post_write shell hook,
// the Worker queue, etc.
//
// Design (see open_source_plan.md §3, "Storage Event Bus"):
//
//	Event emitter (kb publish, hook, Worker, …)
//	        │
//	        ▼
//	   Event bus (in-process, fan-out)
//	        │
//	        ├── (1) DB writer            (mandatory)
//	        ├── (2) SSE fanout           (mandatory)
//	        ├── (3) Notifier             (swappable, default = file)
//	        └── (4) post_write shell hook (configured externally)
//
// Subscribers are invoked synchronously in registration order. A panicking
// subscriber does not bring down the bus; its panic is recovered and logged
// via the bus's logger. Errors from subscribers are aggregated; if no
// subscriber is marked Required, errors are non-fatal to the publisher.
package eventbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// EventKind names a storage event. New kinds may be added; subscribers should
// ignore unknown kinds rather than error out.
type EventKind string

const (
	// KindOnStartup fires once when the server boots, before accepting traffic.
	KindOnStartup EventKind = "on_startup"
	// KindPreRead fires before a virtual file is read. Debounced per path.
	KindPreRead EventKind = "pre_read"
	// KindPostWrite fires after a write has succeeded.
	KindPostWrite EventKind = "post_write"
	// KindTopicRefreshed fires when a Worker run finishes for a content_hash.
	// Used by `kb publish --wait` to unblock.
	KindTopicRefreshed EventKind = "topic_refreshed"
)

// Event is the canonical storage event. It carries the same payload regardless
// of subscriber transport (in-process Go, shell exec, SSE).
type Event struct {
	// Kind names the event type.
	Kind EventKind

	// Path is the virtual filesystem path the event refers to. Empty for
	// startup events.
	Path string

	// Agent is the publishing principal's agent ID. Empty for system events.
	Agent string

	// Partition is the partition slug the event scopes to. Empty if not
	// partition-scoped.
	Partition string

	// ContentHash is a content-addressed identifier for the bytes written.
	// Required for post_write events; empty for others.
	ContentHash string

	// Bytes is the byte count being written. Set for post_write events.
	Bytes int

	// At is the event timestamp. Defaults to time.Now() if zero.
	At time.Time

	// Extra is an optional bag of subscriber-specific metadata. Not used by
	// the core bus.
	Extra map[string]string
}

// Subscriber is invoked for each event. Returning a non-nil error from a
// Required subscriber surfaces as the Publish() error; non-required
// subscribers' errors are logged and dropped.
type Subscriber interface {
	// Name identifies the subscriber in logs; must be stable across restarts.
	Name() string
	// Required marks this subscriber as fatal — bus.Publish returns its error.
	Required() bool
	// Handle processes the event. Should be non-blocking for hot paths; long
	// work should be queued internally.
	Handle(ctx context.Context, e Event) error
}

// SubscriberFunc adapts a function to the Subscriber interface for ad-hoc /
// test subscribers.
type SubscriberFunc struct {
	NameStr  string
	IsReq    bool
	HandleFn func(ctx context.Context, e Event) error
}

func (s SubscriberFunc) Name() string                              { return s.NameStr }
func (s SubscriberFunc) Required() bool                            { return s.IsReq }
func (s SubscriberFunc) Handle(ctx context.Context, e Event) error { return s.HandleFn(ctx, e) }

// Bus is a thread-safe, in-process, ordered fan-out event bus.
type Bus struct {
	mu     sync.RWMutex
	subs   []Subscriber
	logger *slog.Logger
}

// New constructs a fresh Bus. Logger may be nil; defaults to slog.Default().
func New(logger *slog.Logger) *Bus {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bus{logger: logger}
}

// Subscribe appends a subscriber. Subscribers are invoked in subscription
// order. Multiple subscribers with the same Name() are allowed.
func (b *Bus) Subscribe(s Subscriber) {
	if s == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, s)
}

// Subscribers returns the list of currently-registered subscribers (a copy).
// Mostly useful for tests and admin endpoints.
func (b *Bus) Subscribers() []Subscriber {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Subscriber, len(b.subs))
	copy(out, b.subs)
	return out
}

// Publish fans the event out to every subscriber synchronously, in
// subscription order. Required subscribers' errors are aggregated and
// returned. Non-required subscribers' errors are logged and dropped. Panics
// inside subscribers are recovered and logged, never propagated.
func (b *Bus) Publish(ctx context.Context, e Event) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}

	b.mu.RLock()
	subs := make([]Subscriber, len(b.subs))
	copy(subs, b.subs)
	b.mu.RUnlock()

	var errs []error
	for _, s := range subs {
		if err := safeHandle(ctx, s, e, b.logger); err != nil {
			if s.Required() {
				errs = append(errs, fmt.Errorf("%s: %w", s.Name(), err))
			} else {
				b.logger.Warn("subscriber error (non-fatal)",
					"subscriber", s.Name(),
					"event", e.Kind,
					"path", e.Path,
					"err", err,
				)
			}
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// safeHandle invokes s.Handle with panic recovery.
func safeHandle(ctx context.Context, s Subscriber, e Event, logger *slog.Logger) (err error) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("subscriber panicked",
				"subscriber", s.Name(),
				"event", e.Kind,
				"path", e.Path,
				"panic", r,
			)
			err = fmt.Errorf("subscriber %q panicked: %v", s.Name(), r)
		}
	}()
	return s.Handle(ctx, e)
}
