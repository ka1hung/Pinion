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
