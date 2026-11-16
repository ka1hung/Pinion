package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ka1hung/pinion/internal/store"
)

const udpPeerTTL = 5 * time.Minute

type chunk struct {
	b   []byte
	err error
}

type udpPeer struct {
	addr net.Addr
	last time.Time
}

// NetSock is a raw TCP or UDP terminal in client or server mode.
type NetSock struct {
	Proto  string // "tcp" | "udp"
	Config *store.NetConfig

	rd      chan chunk
	pending []byte
	done    chan struct{}

	mu       sync.Mutex
	closed   bool
	notices  chan string
	conn     net.Conn              // client mode
	ln       net.Listener          // tcp server
	pc       net.PacketConn        // udp server
	clients  map[net.Conn]struct{} // tcp server
	peers    map[string]*udpPeer   // udp server
	bindAddr string
}

func (t *NetSock) server() bool { return t.Config != nil && t.Config.Mode == "server" }

func (t *NetSock) addr() string {
	host := ""
	port := 0
	if t.Config != nil {
		host, port = t.Config.Host, t.Config.Port
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Notices implements Notifier.
func (t *NetSock) Notices() <-chan string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.notices == nil {
		t.notices = make(chan string, 64)
	}
	return t.notices
}

func (t *NetSock) notify(format string, a ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.notices == nil {
		return
	}
	select {
	case t.notices <- fmt.Sprintf(format, a...):
	default:
	}
}

func (t *NetSock) push(c chunk) bool {
	select {
	case t.rd <- c:
		return true
	case <-t.done:
		return false
	}
}

func (t *NetSock) Open(ctx context.Context) error {
	if t.Config == nil {
		return errors.New("no network config")
	}
	if t.Proto != "tcp" && t.Proto != "udp" {
		return fmt.Errorf("unknown protocol %q", t.Proto)
	}
	if t.Config.Port <= 0 || t.Config.Port > 65535 {
		return fmt.Errorf("invalid port %d", t.Config.Port)
	}
	t.mu.Lock()
	if t.notices == nil {
		t.notices = make(chan string, 64)
	}
	t.mu.Unlock()
	t.rd = make(chan chunk, 256)
	t.done = make(chan struct{})
	P := strings.ToUpper(t.Proto)
	if !t.server() {
		if strings.TrimSpace(t.Config.Host) == "" {
			return errors.New("host is required")
		}
		d := net.Dialer{Timeout: 15 * time.Second}
		c, err := d.DialContext(ctx, t.Proto, t.addr())
		if err != nil {
			return err
		}
		t.mu.Lock()
		t.conn = c
		t.mu.Unlock()
		if t.Proto == "tcp" {
			t.notify("Connected to %s", c.RemoteAddr())
		} else {
			t.notify("UDP socket %s → %s", c.LocalAddr(), c.RemoteAddr())
		}
		go t.readClient(c)
		return nil
	}
	var lc net.ListenConfig
	if t.Proto == "tcp" {
		ln, err := lc.Listen(ctx, "tcp", t.addr())
		if err != nil {
			return err
		}
		t.mu.Lock()
		t.ln, t.clients, t.bindAddr = ln, map[net.Conn]struct{}{}, ln.Addr().String()
		t.mu.Unlock()
		t.notify("Listening on %s %s", P, displayBind(ln.Addr().String(), t.Config.Host))
		go t.acceptLoop(ln)
		return nil
	}
	pc, err := lc.ListenPacket(ctx, "udp", t.addr())
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.pc, t.peers, t.bindAddr = pc, map[string]*udpPeer{}, pc.LocalAddr().String()
	t.mu.Unlock()
	t.notify("Listening on %s %s", P, displayBind(pc.LocalAddr().String(), t.Config.Host))
	go t.udpLoop(pc)
	return nil
}

func displayBind(actual, host string) string {
	if host == "" {
		_, port, _ := net.SplitHostPort(actual)
		return "0.0.0.0:" + port
	}
	return actual
}

func (t *NetSock) readClient(c net.Conn) {
	for {
		buf := make([]byte, 32*1024)
		n, err := c.Read(buf)
		if n > 0 && !t.push(chunk{b: buf[:n]}) {
			return
		}
		if err != nil {
			var ne *net.OpError
			if t.Proto == "udp" && errors.As(err, &ne) && !t.isClosed() && !errors.Is(err, net.ErrClosed) {
				// ICMP port unreachable etc. — keep the socket open.
				t.notify("UDP: %v", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}
			if errors.Is(err, net.ErrClosed) {
				err = io.EOF
			}
			t.push(chunk{err: err})
			return
		}
	}
}

func (t *NetSock) acceptLoop(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if !t.isClosed() {
				t.push(chunk{err: err})
			}
			return
		}
		t.mu.Lock()
		if t.closed {
			t.mu.Unlock()
			c.Close()
			return
		}
		t.clients[c] = struct{}{}
		n := len(t.clients)
		t.mu.Unlock()
		t.notify("Client %s connected (%d connected)", c.RemoteAddr(), n)
		go func(c net.Conn) {
			buf := make([]byte, 32*1024)
			for {
				n, err := c.Read(buf)
				if n > 0 && !t.push(chunk{b: append([]byte(nil), buf[:n]...)}) {
					return
				}
				if err != nil {
					break
				}
			}
			c.Close()
			t.mu.Lock()
			delete(t.clients, c)
			left := len(t.clients)
			t.mu.Unlock()
			t.notify("Client %s disconnected (%d connected)", c.RemoteAddr(), left)
		}(c)
	}
}

func (t *NetSock) udpLoop(pc net.PacketConn) {
	buf := make([]byte, 65536)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			if t.isClosed() || errors.Is(err, net.ErrClosed) {
				t.push(chunk{err: io.EOF})
				return
			}
			t.notify("UDP: %v", err)
			continue
		}
		key := addr.String()
		t.mu.Lock()
		p := t.peers[key]
		isNew := p == nil
		if isNew {
			p = &udpPeer{addr: addr}
			t.peers[key] = p
		}
		p.last = time.Now()
		t.mu.Unlock()
		if isNew {
			t.notify("UDP peer %s", key)
		}
		if n > 0 && !t.push(chunk{b: append([]byte(nil), buf[:n]...)}) {
			return
		}
	}
}

func (t *NetSock) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

func (t *NetSock) Read(p []byte) (int, error) {
	if t.rd == nil {
		return 0, ErrClosed
	}
	if len(t.pending) > 0 {
		n := copy(p, t.pending)
		t.pending = t.pending[n:]
		return n, nil
	}
	select {
	case c := <-t.rd:
		if c.err != nil {
			return 0, c.err
		}
		n := copy(p, c.b)
		t.pending = c.b[n:]
		return n, nil
	case <-t.done:
		return 0, io.EOF
	}
}

func (t *NetSock) Write(p []byte) (int, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return 0, ErrClosed
	}
	switch {
	case t.conn != nil:
		c := t.conn
		t.mu.Unlock()
		return c.Write(p)
	case t.ln != nil:
		cs := make([]net.Conn, 0, len(t.clients))
		for c := range t.clients {
			cs = append(cs, c)
		}
		t.mu.Unlock()
		if len(cs) == 0 {
			return 0, errors.New("no client connected")
		}
		var firstErr error
		ok := 0
		for _, c := range cs {
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := c.Write(p); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			ok++
		}
		if ok == 0 {
			return 0, firstErr
		}
		return len(p), nil
	case t.pc != nil:
		pc := t.pc
		var addrs []net.Addr
		now := time.Now()
		for k, peer := range t.peers {
			if now.Sub(peer.last) > udpPeerTTL {
				delete(t.peers, k)
				continue
			}
			addrs = append(addrs, peer.addr)
		}
		t.mu.Unlock()
		if len(addrs) == 0 {
			return 0, errors.New("no UDP peer yet — a client must send to this port first")
		}
		for _, a := range addrs {
			if _, err := pc.WriteTo(p, a); err != nil {
				return 0, err
			}
		}
		return len(p), nil
	}
	t.mu.Unlock()
	return 0, ErrClosed
}

func (t *NetSock) Resize(cols, rows int) error { return nil }

func (t *NetSock) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	if t.done != nil {
		close(t.done)
	}
	if t.notices != nil {
		close(t.notices)
	}
	conn, ln, pc := t.conn, t.ln, t.pc
	clients := t.clients
	t.clients = nil
	t.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	if ln != nil {
		ln.Close()
	}
	if pc != nil {
		pc.Close()
	}
	for c := range clients {
		c.Close()
	}
	return nil
}

// Describe implements Info.
func (t *NetSock) Describe() string {
	P := strings.ToUpper(t.Proto)
	if !t.server() {
		return fmt.Sprintf("%s client %s", P, t.addr())
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, port, _ := net.SplitHostPort(t.bindAddr)
	if port == "" && t.Config != nil {
		port = strconv.Itoa(t.Config.Port)
	}
	host := ""
	if t.Config != nil {
		host = t.Config.Host
	}
	if t.Proto == "tcp" {
		n := len(t.clients)
		return fmt.Sprintf("TCP server %s:%s (%d client%s)", host, port, n, plural(n))
	}
	n := len(t.peers)
	return fmt.Sprintf("UDP server %s:%s (%d peer%s)", host, port, n, plural(n))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
