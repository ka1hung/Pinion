package sshx

import (
	"context"
	"errors"
	"sync"

	"github.com/ka1hung/pinion/internal/store"
)

// Pool shares one SSH connection per key (normally the tab ID) between the
// terminal, the SFTP panel, tunnels and sudo-save of that tab.
type Pool struct {
	d     *Dialer
	mu    sync.Mutex
	conns map[string]*poolEntry
}

type poolEntry struct {
	conn      *Conn
	refs      int
	sessionID string
	ready     chan struct{}
	err       error
}

// NewPool creates a pool.
func NewPool(d *Dialer) *Pool { return &Pool{d: d, conns: map[string]*poolEntry{}} }

// Dialer returns the underlying dialer.
func (p *Pool) Dialer() *Dialer { return p.d }

// Acquire returns the live connection for key, dialing if needed, and
// increments its reference count.
func (p *Pool) Acquire(ctx context.Context, key string, sess *store.Session, tabID string) (*Conn, error) {
	for {
		p.mu.Lock()
		e := p.conns[key]
		if e == nil {
			e = &poolEntry{ready: make(chan struct{}), sessionID: sess.ID}
			p.conns[key] = e
			p.mu.Unlock()
			c, err := p.d.Dial(ctx, sess, tabID)
			p.mu.Lock()
			e.conn, e.err = c, err
			close(e.ready)
			if err != nil {
				delete(p.conns, key)
				p.mu.Unlock()
				return nil, err
			}
			e.refs++
			p.mu.Unlock()
			go p.watch(key, e)
			return c, nil
		}
		p.mu.Unlock()
		select {
		case <-e.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if e.err != nil {
			return nil, e.err
		}
		select {
		case <-e.conn.Done():
			// dead; loop to redial (watch() will remove it)
			p.mu.Lock()
			if p.conns[key] == e {
				delete(p.conns, key)
			}
			p.mu.Unlock()
			continue
		default:
		}
		p.mu.Lock()
		e.refs++
		p.mu.Unlock()
		return e.conn, nil
	}
}

func (p *Pool) watch(key string, e *poolEntry) {
	<-e.conn.Done()
	p.mu.Lock()
	if p.conns[key] == e {
		delete(p.conns, key)
	}
	p.mu.Unlock()
}

// Get returns a live connection without changing refs.
func (p *Pool) Get(key string) (*Conn, error) {
	p.mu.Lock()
	e := p.conns[key]
	p.mu.Unlock()
	if e == nil {
		return nil, errors.New("not connected")
	}
	<-e.ready
	if e.err != nil {
		return nil, e.err
	}
	return e.conn, nil
}

// Release drops one reference to c (the connection the caller acquired)
// and closes it when unused. A reference to an older, replaced connection is
// ignored, so a stale holder can never close a newer connection under the
// same key (e.g. SFTP releasing the dead conn after a terminal reconnect).
func (p *Pool) Release(key string, c *Conn) {
	p.mu.Lock()
	e := p.conns[key]
	if e == nil || e.conn == nil || e.conn != c {
		p.mu.Unlock()
		return
	}
	e.refs--
	if e.refs > 0 {
		p.mu.Unlock()
		return
	}
	delete(p.conns, key)
	p.mu.Unlock()
	_ = e.conn.Close()
}

// CloseAll closes every connection.
func (p *Pool) CloseAll() {
	p.mu.Lock()
	es := p.conns
	p.conns = map[string]*poolEntry{}
	p.mu.Unlock()
	for _, e := range es {
		if e.conn != nil {
			_ = e.conn.Close()
		}
	}
}
