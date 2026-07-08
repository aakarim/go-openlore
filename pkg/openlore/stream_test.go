package openlore

import (
	"bufio"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestStream_EmitRecentAndFilter(t *testing.T) {
	s := NewStream()
	ctx := context.Background()
	_ = s.Emit(ctx, Event{Kind: KindPostWrite, Path: "/a", Partition: "p1"})
	_ = s.Emit(ctx, Event{Kind: KindPostWrite, Path: "/b", Partition: "p2"})
	_ = s.Emit(ctx, Event{Kind: KindPostWrite, Path: "/c", Partition: "p1"})

	all := s.Recent(MatchAll, 0)
	if len(all) != 3 {
		t.Fatalf("Recent(all) = %d, want 3", len(all))
	}
	if all[len(all)-1].Path != "/c" {
		t.Fatalf("Recent newest-last: got %q", all[len(all)-1].Path)
	}

	p1 := s.Recent(MatchPartition("p1"), 0)
	if len(p1) != 2 {
		t.Fatalf("Recent(p1) = %d, want 2", len(p1))
	}

	last1 := s.Recent(MatchAll, 1)
	if len(last1) != 1 || last1[0].Path != "/c" {
		t.Fatalf("Recent(all,1) = %+v", last1)
	}
}

func TestStream_EmitDefaultsAt(t *testing.T) {
	s := NewStream()
	_ = s.Emit(context.Background(), Event{Kind: KindPostWrite})
	if got := s.Recent(MatchAll, 1); len(got) != 1 || got[0].At.IsZero() {
		t.Fatalf("Emit should default At: %+v", got)
	}
}

func TestStream_RingEviction(t *testing.T) {
	s := NewStream(WithRingSize(2))
	ctx := context.Background()
	_ = s.Emit(ctx, Event{Path: "/1"})
	_ = s.Emit(ctx, Event{Path: "/2"})
	_ = s.Emit(ctx, Event{Path: "/3"})
	got := s.Recent(MatchAll, 0)
	if len(got) != 2 || got[0].Path != "/2" || got[1].Path != "/3" {
		t.Fatalf("ring eviction wrong: %+v", got)
	}
}

func TestStream_SubscribeLiveDelivery(t *testing.T) {
	s := NewStream()
	ch, cancel := s.Subscribe(MatchPartition("p1"))
	defer cancel()

	_ = s.Emit(context.Background(), Event{Path: "/x", Partition: "p2"}) // filtered out
	_ = s.Emit(context.Background(), Event{Path: "/y", Partition: "p1"})

	select {
	case e := <-ch:
		if e.Path != "/y" {
			t.Fatalf("got %q, want /y (p2 should be filtered)", e.Path)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for filtered event")
	}
}

func TestStream_CancelStopsDelivery(t *testing.T) {
	s := NewStream()
	ch, cancel := s.Subscribe(MatchAll)
	if s.ClientCount() != 1 {
		t.Fatalf("ClientCount = %d, want 1", s.ClientCount())
	}
	cancel()
	if s.ClientCount() != 0 {
		t.Fatalf("ClientCount after cancel = %d, want 0", s.ClientCount())
	}
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after cancel")
	}
	// Double cancel is safe.
	cancel()
}

func TestStream_SlowClientDropsNotBlocks(t *testing.T) {
	s := NewStream(WithClientBuffer(1))
	_, cancel := s.Subscribe(MatchAll)
	defer cancel()
	// Emit far more than the buffer; must not block.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			_ = s.Emit(context.Background(), Event{Path: "/x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Emit blocked on a slow client")
	}
}

func TestStream_OpenReaderTails(t *testing.T) {
	s := NewStream()
	ctx, cancel := context.WithCancel(context.Background())
	rc := s.OpenReader(ctx, MatchAll)
	defer rc.Close()

	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = s.Emit(context.Background(), Event{Kind: KindPostWrite, Path: "/tailed"})
	}()

	sc := bufio.NewScanner(rc)
	lineCh := make(chan string, 1)
	go func() {
		if sc.Scan() {
			lineCh <- sc.Text()
		}
	}()

	select {
	case line := <-lineCh:
		var got map[string]any
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("line not JSON: %v (%q)", err, line)
		}
		if got["path"] != "/tailed" {
			t.Fatalf("tailed event path = %v", got["path"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out tailing OpenReader")
	}
	cancel()
}

func TestEncodeEvent_Shape(t *testing.T) {
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	b, err := EncodeEvent(Event{
		Kind: KindPostWrite, Path: "/p", Agent: "a", Partition: "part",
		ContentHash: "h", Bytes: 42, At: at, Extra: map[string]string{"k": "v"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{
		"kind": "post_write", "path": "/p", "agent": "a", "partition": "part",
		"content_hash": "h", "bytes": float64(42), "at": "2026-01-02T03:04:05Z",
	} {
		if got[k] != want {
			t.Errorf("field %q = %v, want %v", k, got[k], want)
		}
	}
}
