// Package tunnel implements SSH port forwarding: Local (-L), Remote (-R) and
// Dynamic (-D, SOCKS5/SOCKS4a) over an established *ssh.Client.
package tunnel

import (
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"golang.org/x/crypto/ssh"

	"github.com/ka1hung/pinion/internal/store"
)

// Stats are live counters of a forwarder.
type Stats struct {
	Conns    int   `json:"conns"`
	BytesIn  int64 `json:"bytesIn"`  // received from the far side
	BytesOut int64 `json:"bytesOut"` // sent to the far side
}

// Forwarder runs one tunnel.
type Forwarder struct {
	T      store.Tunnel
	client *ssh.Client
	ln     net.Listener

	in, out atomic.Int64
	mu      sync.Mutex
	conns   map[net.Conn]struct{}
	done    chan struct{}
	once    sync.Once
	err     error
}

// BindAddr returns the effective listen address of a tunnel.
func BindAddr(t store.Tunnel) string {
	h := strings.TrimSpace(t.BindHost)
	if h == "" {
		h = "127.0.0.1"
	}
	return net.JoinHostPort(h, strconv.Itoa(t.BindPort))
}

// TargetAddr returns host:port of the target.
func TargetAddr(t store.Tunnel) string {
	return net.JoinHostPort(strings.TrimSpace(t.TargetHost), strconv.Itoa(t.TargetPort))
}

// Validate checks a tunnel definition.
func Validate(t store.Tunnel) error {
	switch t.Kind {
	case "L", "R":
		if strings.TrimSpace(t.TargetHost) == "" || t.TargetPort <= 0 || t.TargetPort > 65535 {
			return errors.New("target host and port are required")
		}
	case "D":
	default:
		return fmt.Errorf("unknown tunnel kind %q (want L, R or D)", t.Kind)
	}
	if t.BindPort < 0 || t.BindPort > 65535 || (t.BindPort == 0 && t.Kind == "R") {
		return errors.New("invalid bind port")
	}
	return nil
}

// Start opens the listener and begins forwarding.
func Start(client *ssh.Client, t store.Tunnel) (*Forwarder, error) {
	if err := Validate(t); err != nil {
		return nil, err
	}
	f := &Forwarder{T: t, client: client, conns: map[net.Conn]struct{}{}, done: make(chan struct{})}
	var err error
	switch t.Kind {
	case "L", "D":
		f.ln, err = net.Listen("tcp", BindAddr(t))
		if err != nil {
			return nil, FriendlyBindError(BindAddr(t), err)
		}
	case "R":
		f.ln, err = client.Listen("tcp", BindAddr(t))
		if err != nil {
			return nil, fmt.Errorf("remote listen on %s refused by server (check AllowTcpForwarding / GatewayPorts, or port in use): %w", BindAddr(t), err)
		}
	}
	go f.acceptLoop()
	return f, nil
}

// Addr returns the actual listen address (useful when BindPort was 0).
func (f *Forwarder) Addr() net.Addr { return f.ln.Addr() }

// Done is closed when the forwarder stops.
func (f *Forwarder) Done() <-chan struct{} { return f.done }

// Err returns why the forwarder stopped (nil when stopped by Stop).
func (f *Forwarder) Err() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.err
}

// Stats returns live counters.
func (f *Forwarder) Stats() Stats {
	f.mu.Lock()
	n := len(f.conns)
	f.mu.Unlock()
	return Stats{Conns: n, BytesIn: f.in.Load(), BytesOut: f.out.Load()}
}

// Stop closes the listener and all connections.
func (f *Forwarder) Stop() { f.stop(nil) }

func (f *Forwarder) stop(err error) {
	f.once.Do(func() {
		f.mu.Lock()
		f.err = err
		conns := f.conns
		f.conns = map[net.Conn]struct{}{}
		f.mu.Unlock()
		_ = f.ln.Close()
		for c := range conns {
			_ = c.Close()
		}
		close(f.done)
	})
}

func (f *Forwarder) track(c net.Conn) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.done:
		return false
	default:
	}
	f.conns[c] = struct{}{}
	return true
}

func (f *Forwarder) untrack(c net.Conn) {
	f.mu.Lock()
	delete(f.conns, c)
	f.mu.Unlock()
	_ = c.Close()
}

func (f *Forwarder) acceptLoop() {
	for {
		c, err := f.ln.Accept()
		if err != nil {
			select {
			case <-f.done:
			default:
				f.stop(fmt.Errorf("listener closed: %w", err))
			}
			return
		}
		go f.handle(c)
	}
}

func (f *Forwarder) handle(c net.Conn) {
	if !f.track(c) {
		_ = c.Close()
		return
	}
	defer f.untrack(c)
	local := c
	var (
		remote net.Conn
		err    error
	)
	switch f.T.Kind {
	case "L":
		remote, err = f.client.Dial("tcp", TargetAddr(f.T))
	case "R":
		remote, err = net.Dial("tcp", TargetAddr(f.T))
	case "D":
		var target string
		var reply func(error) error
		var bc net.Conn
		target, reply, bc, err = Handshake(c)
		if err != nil {
			return
		}
		local = bc
		remote, err = f.client.Dial("tcp", target)
		if rerr := reply(err); rerr != nil && err == nil {
			_ = remote.Close()
			return
		}
	}
	if err != nil {
		return
	}
	if !f.track(remote) {
		_ = remote.Close()
		return
	}
	defer f.untrack(remote)
	f.pipe(local, remote)
}

type countWriter struct {
	w io.Writer
	n *atomic.Int64
}

func (cw countWriter) Write(p []byte) (int, error) {
	n, err := cw.w.Write(p)
	cw.n.Add(int64(n))
	return n, err
}

// pipe copies both ways; local is the accepted side for L/D, remote the
// dialed side. For R, "local" is the remote-accepted channel.
func (f *Forwarder) pipe(local, far net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(countWriter{far, &f.out}, local)
		closeWrite(far)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(countWriter{local, &f.in}, far)
		closeWrite(local)
	}()
	wg.Wait()
}

func closeWrite(c net.Conn) {
	type cw interface{ CloseWrite() error }
	if x, ok := c.(cw); ok {
		_ = x.CloseWrite()
		return
	}
	_ = c.Close()
}

// FriendlyBindError explains common local listen failures.
func FriendlyBindError(addr string, err error) error {
	switch {
	case errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use") ||
		strings.Contains(err.Error(), "Only one usage of each socket address"):
		return fmt.Errorf("cannot listen on %s: port already in use by another program", addr)
	case errors.Is(err, syscall.EACCES) || strings.Contains(strings.ToLower(err.Error()), "permission denied") ||
		strings.Contains(err.Error(), "forbidden by its access permissions"):
		hint := "choose a port above 1024"
		switch runtime.GOOS {
		case "linux":
			hint = "ports below 1024 need root on Linux: choose a port above 1024, or run `sudo setcap cap_net_bind_service=+ep <path to Pinion>`"
		case "windows":
			hint = "the port may be reserved by Windows or blocked by policy; choose another port"
		}
		return fmt.Errorf("cannot listen on %s: permission denied (%s)", addr, hint)
	case strings.Contains(err.Error(), "cannot assign requested address") || strings.Contains(err.Error(), "requested address is not valid"):
		return fmt.Errorf("cannot listen on %s: that IP address is not on this computer (network interface down?)", addr)
	}
	return fmt.Errorf("cannot listen on %s: %w", addr, err)
}
