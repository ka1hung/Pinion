package sshx

import "testing"

// A stale reference to a replaced connection must not affect the new one.
func TestReleaseIgnoresStaleConn(t *testing.T) {
	p := &Pool{conns: map[string]*poolEntry{}}
	oldC := &Conn{done: make(chan struct{})}
	newC := &Conn{done: make(chan struct{})}
	ready := make(chan struct{})
	close(ready)
	p.conns["t1"] = &poolEntry{conn: newC, refs: 1, ready: ready}
	p.Release("t1", oldC)
	if e := p.conns["t1"]; e == nil || e.refs != 1 {
		t.Fatal("stale release changed the live connection")
	}
}
