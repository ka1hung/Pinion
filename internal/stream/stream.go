// Package stream carries terminal byte streams between Go and the WebView
// over a local WebSocket (binary frames, no JSON wrapping). The server binds
// only to 127.0.0.1 on a random port and requires a per-launch random token.
//
// Output is sent as soon as it arrives when the channel is idle (so keystroke
// echo is not delayed), and coalesced to at most one flush per 16ms while
// output keeps coming (cat of a large file, boot logs) so the WebView is not
// flooded with tiny messages.
//
// Flow control: the UI sends {"t":"pause"} when xterm.js falls behind and
// {"t":"resume"} once it has caught up. Producers call Channel.Wait before
// reading more from their source, so the backpressure reaches the PTY / socket
// instead of piling up unbounded in the WebView.
package stream

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	flushInterval = 16 * time.Millisecond
	replayLimit   = 512 * 1024
	maxFrame      = 256 * 1024
)

// Handler receives input from the UI for one channel.
type Handler interface {
	Input(p []byte)
	Resize(cols, rows int)
}

// Channel is one logical stream (one tab).
type Channel struct {
	id      string
	handler Handler

	mu      sync.Mutex
	replay  []byte // ring of recent output for re-attach
	pending []byte
	subs    map[*subscriber]struct{}
	closed  bool

	kick     chan struct{} // buffered(1): pending output to flush
	done     chan struct{} // closed with the channel
	paused   int           // subscribers currently asking for a pause
	unpaused chan struct{} // closed while paused == 0
}

func newChannel(id string, handler Handler) *Channel {
	c := &Channel{
		id:       id,
		handler:  handler,
		subs:     map[*subscriber]struct{}{},
		kick:     make(chan struct{}, 1),
		done:     make(chan struct{}),
		unpaused: make(chan struct{}),
	}
	close(c.unpaused)
	go c.flushLoop()
	return c
}

// flushLoop sends output on the leading edge, then throttles: after a flush
// it waits flushInterval before the next one, coalescing whatever arrived.
func (c *Channel) flushLoop() {
	t := time.NewTimer(0)
	<-t.C
	for {
		select {
		case <-c.done:
			return
		case <-c.kick:
		}
		c.flush()
		t.Reset(flushInterval)
		select {
		case <-c.done:
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// Wait blocks while the UI has asked for a pause. It returns early when the
// channel closes or ctx is done. Producers call it before reading more input.
func (c *Channel) Wait(ctx context.Context) {
	c.mu.Lock()
	ch := c.unpaused
	c.mu.Unlock()
	select {
	case <-ch:
	case <-c.done:
	case <-ctx.Done():
	}
}

// setPaused records a subscriber's pause state. Caller must not hold c.mu.
func (c *Channel) setPaused(s *subscriber, on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if s.paused == on {
		return
	}
	s.paused = on
	if on {
		if c.paused == 0 {
			c.unpaused = make(chan struct{})
		}
		c.paused++
		return
	}
	c.paused--
	if c.paused == 0 {
		close(c.unpaused)
	}
}

type subscriber struct {
	// skip is how many bytes at the start of the next flush the subscriber
	// already received through the replay at attach time.
	skip   int
	paused bool // guarded by Channel.mu
	out    chan []byte
	done   chan struct{}
	once   sync.Once
}

func (s *subscriber) close() { s.once.Do(func() { close(s.done) }) }

// Write queues output for the UI. Safe for concurrent use.
func (c *Channel) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return len(p), nil
	}
	c.pending = append(c.pending, p...)
	c.replay = append(c.replay, p...)
	if len(c.replay) > replayLimit {
		// keep the tail; copy so the backing array does not grow forever
		tail := c.replay[len(c.replay)-replayLimit/2:]
		c.replay = append(make([]byte, 0, replayLimit), tail...)
	}
	select {
	case c.kick <- struct{}{}:
	default:
	}
	return len(p), nil
}

// ClearReplay drops the stored scrollback (e.g. on "clear").
func (c *Channel) ClearReplay() {
	c.mu.Lock()
	c.replay = nil
	c.mu.Unlock()
}

func (c *Channel) flush() {
	c.mu.Lock()
	if len(c.pending) == 0 || len(c.subs) == 0 {
		if len(c.subs) == 0 {
			c.pending = c.pending[:0]
		}
		c.mu.Unlock()
		return
	}
	data := c.pending
	c.pending = nil
	subs := make([]*subscriber, 0, len(c.subs))
	for s := range c.subs {
		subs = append(subs, s)
	}
	c.mu.Unlock()
	for _, s := range subs {
		c.mu.Lock()
		d := data
		if s.skip > 0 {
			d = d[min(s.skip, len(d)):]
			s.skip = 0
		}
		c.mu.Unlock()
		for len(d) > 0 {
			n := min(len(d), maxFrame)
			select {
			case s.out <- d[:n]:
			default:
				// subscriber too slow: drop it, the UI will reconnect and replay
				s.close()
				d = nil
				continue
			}
			d = d[n:]
		}
	}
}

// Hub owns all channels and the WebSocket server.
type Hub struct {
	token string
	ln    net.Listener
	srv   *http.Server

	mu       sync.RWMutex
	channels map[string]*Channel
	stop     chan struct{}
}

// NewHub starts the server on 127.0.0.1:0.
func NewHub() (*Hub, error) {
	var tb [32]byte
	if _, err := rand.Read(tb[:]); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	h := &Hub{
		token:    hex.EncodeToString(tb[:]),
		ln:       ln,
		channels: map[string]*Channel{},
		stop:     make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws/{id}", h.serveWS)
	h.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = h.srv.Serve(ln) }()
	return h, nil
}

// Port returns the listening port.
func (h *Hub) Port() int { return h.ln.Addr().(*net.TCPAddr).Port }

// Token returns the per-launch auth token.
func (h *Hub) Token() string { return h.token }

// Close stops the server.
func (h *Hub) Close() error {
	select {
	case <-h.stop:
	default:
		close(h.stop)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return h.srv.Shutdown(ctx)
}

// Open registers a channel.
func (h *Hub) Open(id string, handler Handler) *Channel {
	c := newChannel(id, handler)
	h.mu.Lock()
	if old := h.channels[id]; old != nil {
		h.closeChannel(old)
	}
	h.channels[id] = c
	h.mu.Unlock()
	return c
}

// Get returns a channel by id.
func (h *Hub) Get(id string) *Channel {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.channels[id]
}

func (h *Hub) closeChannel(c *Channel) {
	c.mu.Lock()
	if !c.closed {
		close(c.done)
	}
	c.closed = true
	for s := range c.subs {
		s.close()
	}
	c.subs = nil
	c.mu.Unlock()
}

// CloseChannel removes a channel and disconnects its subscribers.
func (h *Hub) CloseChannel(id string) {
	h.mu.Lock()
	c := h.channels[id]
	delete(h.channels, id)
	h.mu.Unlock()
	if c != nil {
		h.closeChannel(c)
	}
}

var originPatterns = []string{
	"wails", "wails.localhost", "wails.localhost:*",
	"localhost", "localhost:*", "127.0.0.1", "127.0.0.1:*",
}

type control struct {
	T    string `json:"t"`
	Cols int    `json:"c"`
	Rows int    `json:"r"`
}

func (h *Hub) serveWS(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if subtle.ConstantTimeCompare([]byte(tok), []byte(h.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	c := h.Get(r.PathValue("id"))
	if c == nil {
		http.Error(w, "no such stream", http.StatusNotFound)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: originPatterns})
	if err != nil {
		return
	}
	conn.SetReadLimit(4 << 20)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sub := &subscriber{out: make(chan []byte, 1024), done: make(chan struct{})}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		conn.Close(websocket.StatusGoingAway, "closed")
		return
	}
	replay := append([]byte(nil), c.replay...)
	sub.skip = len(c.pending) // already part of the replay
	c.subs[sub] = struct{}{}
	c.mu.Unlock()
	defer func() {
		c.setPaused(sub, false)
		c.mu.Lock()
		delete(c.subs, sub)
		c.mu.Unlock()
		sub.close()
	}()

	// writer
	go func() {
		defer cancel()
		if len(replay) > 0 {
			for len(replay) > 0 {
				n := min(len(replay), maxFrame)
				if err := conn.Write(ctx, websocket.MessageBinary, replay[:n]); err != nil {
					return
				}
				replay = replay[n:]
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-sub.done:
				conn.Close(websocket.StatusGoingAway, "stream closed")
				return
			case b := <-sub.out:
				if err := conn.Write(ctx, websocket.MessageBinary, b); err != nil {
					return
				}
			}
		}
	}()

	// reader
	for {
		typ, data, err := conn.Read(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				conn.Close(websocket.StatusNormalClosure, "")
			}
			return
		}
		switch typ {
		case websocket.MessageBinary:
			c.handler.Input(data)
		case websocket.MessageText:
			var m control
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			switch strings.ToLower(m.T) {
			case "resize":
				if m.Cols > 0 && m.Rows > 0 {
					c.handler.Resize(m.Cols, m.Rows)
				}
			case "pause":
				c.setPaused(sub, true)
			case "resume":
				c.setPaused(sub, false)
			}
		}
	}
}
