package openlore

import (
	"context"
	"time"
)

// EventKind names a storage event. New kinds may be added; consumers should
// ignore unknown kinds rather than error out.
type EventKind string

const (
	// KindOnStartup fires once when the server boots, before accepting traffic.
	KindOnStartup EventKind = "on_startup"
	// KindPreRead fires before a virtual file is read.
	KindPreRead EventKind = "pre_read"
	// KindPostWrite fires after a write has succeeded.
	KindPostWrite EventKind = "post_write"
	// KindPostDelete fires after a delete (rm / rm -r) has succeeded.
	KindPostDelete EventKind = "post_delete"
	// KindTopicRefreshed fires when a processing run finishes for a
	// content_hash. Emitted for observability on the feed.
	KindTopicRefreshed EventKind = "topic_refreshed"
)

// Event is the canonical storage event. It carries the same payload regardless
// of transport (in-process Go, SSE, SSH tail).
type Event struct {
	// Kind names the event type.
	Kind EventKind
	// Path is the virtual filesystem path the event refers to. Empty for
	// startup / non-FS events.
	Path string
	// Agent is the publishing principal's agent ID. Empty for system events.
	Agent string
	// Partition is the partition slug the event scopes to. Empty if not
	// partition-scoped.
	Partition string
	// ContentHash is a content-addressed identifier for the bytes written.
	ContentHash string
	// Bytes is the byte count being written. Set for post_write events.
	Bytes int
	// At is the event timestamp. Defaults to time.Now() if zero.
	At time.Time
	// Extra is an optional bag of consumer-specific metadata.
	Extra map[string]string
}

// Emit is the append-only sink for storage events. It is deliberately NOT a
// subscriber bus: it fans events into an in-memory tailable stream (ring +
// live readers) only. All routing, queueing, notification, and coordination
// policy lives in the host application (e.g. the knowledge backend), which
// calls Emit from its own write and domain code.
type Emit interface {
	// Emit appends the event to the stream. Implementations default At when
	// zero. Errors are limited to stream lifecycle / encoding problems; a
	// slow or absent reader is never an error.
	Emit(ctx context.Context, e Event) error
}

// EmitFunc adapts a function to the Emit interface.
type EmitFunc func(ctx context.Context, e Event) error

// Emit implements Emit.
func (f EmitFunc) Emit(ctx context.Context, e Event) error { return f(ctx, e) }

// EventFilter reports whether an event should be delivered to a reader.
type EventFilter func(Event) bool

// MatchAll delivers every event.
func MatchAll(Event) bool { return true }

// MatchPartition delivers only events for the given partition slug. An empty
// slug matches all events.
func MatchPartition(partition string) EventFilter {
	return func(e Event) bool {
		return partition == "" || e.Partition == partition
	}
}
