package stream

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type rec struct {
	mu   sync.Mutex
	in   []byte
	cols int
}

func (r *rec) Input(p []byte)  { r.mu.Lock(); r.in = append(r.in, p...); r.mu.Unlock() }
func (r *rec) Resize(c, _ int) { r.mu.Lock(); r.cols = c; r.mu.Unlock() }

func TestHubAuthAndRoundTrip(t *testing.T) {
	h, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	r := &rec{}
	ch := h.Open("t1", r)
	ch.Write([]byte("before-attach "))

	base := fmt.Sprintf("ws://127.0.0.1:%d/ws/t1", h.Port())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// wrong token is rejected before upgrade
	_, resp, err := websocket.Dial(ctx, base+"?token=bad", nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %v %v", resp, err)
	}
	// foreign web origin is rejected
	_, _, err = websocket.Dial(ctx, base+"?token="+h.Token(), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example"}}})
	if err == nil {
		t.Fatal("expected origin rejection")
	}

	c, _, err := websocket.Dial(ctx, base+"?token="+h.Token(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_, b, err := c.Read(ctx) // replay
	if err != nil || string(b) != "before-attach " {
		t.Fatalf("replay %q %v", b, err)
	}
	ch.Write([]byte("live"))
	_, b, err = c.Read(ctx)
	if err != nil || string(b) != "live" {
		t.Fatalf("live %q %v", b, err)
	}
	c.Write(ctx, websocket.MessageBinary, []byte("ls\r"))
	c.Write(ctx, websocket.MessageText, []byte(`{"t":"resize","c":132,"r":40}`))
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		ok := string(r.in) == "ls\r" && r.cols == 132
		r.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("input not delivered: %q cols=%d", r.in, r.cols)
}

func TestHubBindsLoopbackOnly(t *testing.T) {
	h, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if a := h.ln.Addr().String(); a[:10] != "127.0.0.1:" {
		t.Fatalf("listening on %s", a)
	}
	if len(h.Token()) != 64 {
		t.Fatal("token should be 32 random bytes hex")
	}
}

func dialHub(t *testing.T, h *Hub, id string) (*websocket.Conn, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	c, _, err := websocket.Dial(ctx, fmt.Sprintf("ws://127.0.0.1:%d/ws/%s?token=%s", h.Port(), id, h.Token()), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetReadLimit(1 << 20)
	t.Cleanup(func() { c.CloseNow() })
	return c, ctx
}

// An idle channel must deliver output immediately (keystroke echo), not on
// the next 16ms tick.
func TestIdleOutputIsNotDelayed(t *testing.T) {
	h, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ch := h.Open("e", &rec{})
	c, ctx := dialHub(t, h, "e")
	time.Sleep(50 * time.Millisecond) // attach settles

	var worst time.Duration
	for i := 0; i < 20; i++ {
		time.Sleep(30 * time.Millisecond) // idle longer than flushInterval
		start := time.Now()
		ch.Write([]byte("x"))
		if _, _, err := c.Read(ctx); err != nil {
			t.Fatal(err)
		}
		worst = max(worst, time.Since(start))
	}
	t.Logf("worst idle echo latency: %v", worst)
	if worst > 8*time.Millisecond {
		t.Fatalf("idle echo took %v", worst)
	}
}

// Under a burst, writes are still coalesced into few frames.
func TestBurstIsCoalesced(t *testing.T) {
	h, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ch := h.Open("b", &rec{})
	c, ctx := dialHub(t, h, "b")
	time.Sleep(50 * time.Millisecond)

	const writes, size = 2000, 100
	go func() {
		for i := 0; i < writes; i++ {
			ch.Write(make([]byte, size))
		}
	}()
	frames, got := 0, 0
	for got < writes*size {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		frames++
		got += len(b)
	}
	t.Logf("%d writes -> %d frames", writes, frames)
	if frames > writes/10 {
		t.Fatalf("burst not coalesced: %d frames", frames)
	}
}

func TestPauseBlocksProducer(t *testing.T) {
	h, err := NewHub()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ch := h.Open("p", &rec{})
	c, ctx := dialHub(t, h, "p")

	c.Write(ctx, websocket.MessageText, []byte(`{"t":"pause"}`))
	waitBlocked := func() bool {
		done := make(chan struct{})
		go func() { ch.Wait(context.Background()); close(done) }()
		select {
		case <-done:
			return false
		case <-time.After(100 * time.Millisecond):
			return true
		}
	}
	// give the reader a moment to apply the pause
	deadline := time.Now().Add(time.Second)
	for !waitBlocked() {
		if time.Now().After(deadline) {
			t.Fatal("pause not applied")
		}
	}
	c.Write(ctx, websocket.MessageText, []byte(`{"t":"resume"}`))
	done := make(chan struct{})
	go func() { ch.Wait(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("resume did not release producer")
	}

	// a paused subscriber that disconnects must not leave the producer stuck
	c.Write(ctx, websocket.MessageText, []byte(`{"t":"pause"}`))
	time.Sleep(50 * time.Millisecond)
	c.Close(websocket.StatusNormalClosure, "")
	done2 := make(chan struct{})
	go func() { ch.Wait(context.Background()); close(done2) }()
	select {
	case <-done2:
	case <-time.After(time.Second):
		t.Fatal("disconnect did not release producer")
	}
}
