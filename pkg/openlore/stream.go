package openlore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Stream is an in-memory, tailable event stream: the mechanism half of the
// storage-event system. It implements Emit (append side) and offers three
// read transports over the same payload — a recent-events ring snapshot, a
// live Go channel subscription, and an io.ReadCloser for `tail -f`-style
// consumers — plus an SSE HTTP handler.
//
// A Stream is intentionally ephemeral and lossy for slow readers: it is not a
// durable log, a queue, or a synchronization primitive. Host applications that
// need durable processing, queueing, or wait-coordination must implement those
// directly, not by subscribing to a Stream.
type Stream struct {
	mu      sync.RWMutex
	clients map[*streamClient]struct{}
	ring    []Event
	ringMax int
	// clientBuf is the per-client channel buffer; a client that falls this far
	// behind drops events rather than blocking the emitter.
	clientBuf int
}

// StreamOption configures a Stream.
type StreamOption func(*Stream)

// WithRingSize sets the recent-events ring buffer capacity (default 256).
func WithRingSize(n int) StreamOption {
	return func(s *Stream) {
		if n > 0 {
			s.ringMax = n
		}
	}
}

// WithClientBuffer sets the per-reader channel buffer (default 64).
func WithClientBuffer(n int) StreamOption {
	return func(s *Stream) {
		if n > 0 {
			s.clientBuf = n
		}
	}
}

// NewStream constructs an empty Stream.
func NewStream(opts ...StreamOption) *Stream {
	s := &Stream{
		clients:   make(map[*streamClient]struct{}),
		ringMax:   256,
		clientBuf: 64,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Emit appends the event to the ring and non-blockingly delivers it to every
// matching live reader. It implements Emit. Slow readers drop the event.
func (s *Stream) Emit(_ context.Context, e Event) error {
	if e.At.IsZero() {
		e.At = time.Now()
	}

	s.mu.Lock()
	s.ring = append(s.ring, e)
	if len(s.ring) > s.ringMax {
		s.ring = s.ring[len(s.ring)-s.ringMax:]
	}
	clients := make([]*streamClient, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()

	for _, c := range clients {
		if c.filter(e) {
			select {
			case c.ch <- e:
			default:
				// Slow reader: drop the event. The reader sees a gap rather
				// than blocking the emitter (back-pressure by loss).
			}
		}
	}
	return nil
}

// Recent returns up to n most-recent matching events (newest last). n <= 0
// returns all matching events in the ring.
func (s *Stream) Recent(filter EventFilter, n int) []Event {
	if filter == nil {
		filter = MatchAll
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Event, 0, len(s.ring))
	for _, e := range s.ring {
		if filter(e) {
			out = append(out, e)
		}
	}
	if n > 0 && n < len(out) {
		out = out[len(out)-n:]
	}
	return out
}

// Subscribe registers a live reader filtered by filter (nil = all). It returns
// a channel of events and a cancel function the caller MUST invoke to release
// the subscription.
func (s *Stream) Subscribe(filter EventFilter) (<-chan Event, func()) {
	if filter == nil {
		filter = MatchAll
	}
	c := &streamClient{
		ch:     make(chan Event, s.clientBuf),
		filter: filter,
	}
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	return c.ch, func() {
		s.mu.Lock()
		if _, ok := s.clients[c]; ok {
			delete(s.clients, c)
			close(c.ch)
		}
		s.mu.Unlock()
	}
}

// ClientCount returns the number of currently-connected live readers.
func (s *Stream) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// OpenReader returns a tail-style io.ReadCloser that streams matching events,
// one JSON object per line (terminated by '\n', encoded by EncodeEvent).
// Read blocks until the next event or ctx cancellation; it returns io.EOF when
// the context is cancelled or the stream reader closes. Close is mandatory.
func (s *Stream) OpenReader(ctx context.Context, filter EventFilter) io.ReadCloser {
	ch, cancel := s.Subscribe(filter)
	streamCtx, ctxCancel := context.WithCancel(ctx)

	out := make(chan []byte, s.clientBuf)
	go func() {
		defer close(out)
		for {
			select {
			case <-streamCtx.Done():
				return
			case e, ok := <-ch:
				if !ok {
					return
				}
				body, err := EncodeEvent(e)
				if err != nil {
					continue
				}
				select {
				case out <- append(body, '\n'):
				case <-streamCtx.Done():
					return
				}
			}
		}
	}()

	return &streamReader{
		ctx:       streamCtx,
		cancelCtx: ctxCancel,
		source:    out,
		stopFn:    cancel,
	}
}

// HTTPHandler returns an SSE handler. filterFromRequest derives the per-request
// filter (e.g. from a `partition` query param); nil yields MatchAll.
func (s *Stream) HTTPHandler(filterFromRequest func(*http.Request) EventFilter) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher.Flush()

		var filter EventFilter = MatchAll
		if filterFromRequest != nil {
			if f := filterFromRequest(r); f != nil {
				filter = f
			}
		}
		ch, cancel := s.Subscribe(filter)
		defer cancel()

		ka := time.NewTicker(15 * time.Second)
		defer ka.Stop()

		for {
			select {
			case <-r.Context().Done():
				return
			case <-ka.C:
				_, _ = fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
			case e, ok := <-ch:
				if !ok {
					return
				}
				if err := writeSSE(w, e); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
}

// EncodeEvent serialises an Event to the canonical JSON wire format shared by
// every transport (SSE, SSH tail, file notifier).
func EncodeEvent(e Event) ([]byte, error) {
	return json.Marshal(map[string]any{
		"kind":         string(e.Kind),
		"path":         e.Path,
		"agent":        e.Agent,
		"partition":    e.Partition,
		"content_hash": e.ContentHash,
		"bytes":        e.Bytes,
		"at":           e.At.UTC().Format(time.RFC3339Nano),
		"extra":        e.Extra,
	})
}

func writeSSE(w http.ResponseWriter, e Event) error {
	body, err := EncodeEvent(e)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Kind, body)
	return err
}

// streamClient is one live reader.
type streamClient struct {
	ch     chan Event
	filter EventFilter
}

// streamReader is the tail-style io.ReadCloser returned by OpenReader.
type streamReader struct {
	ctx       context.Context
	cancelCtx context.CancelFunc
	source    <-chan []byte
	stopFn    func()
	pending   bytes.Buffer
	mu        sync.Mutex
	closed    bool
}

func (r *streamReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if r.pending.Len() > 0 {
		n, _ := r.pending.Read(p)
		r.mu.Unlock()
		return n, nil
	}
	r.mu.Unlock()

	select {
	case <-r.ctx.Done():
		return 0, io.EOF
	case b, ok := <-r.source:
		if !ok {
			return 0, io.EOF
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		n := copy(p, b)
		if n < len(b) {
			r.pending.Write(b[n:])
		}
		return n, nil
	}
}

func (r *streamReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.cancelCtx()
	r.stopFn()
	return nil
}
