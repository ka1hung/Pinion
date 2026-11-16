package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ka1hung/pinion/internal/sshx"
	"github.com/ka1hung/pinion/internal/store"
	"github.com/ka1hung/pinion/internal/tunnel"
)

// TunnelService manages SSH port forwards (L/R/D) defined on SSH sessions.
// Tunnels run on their own pooled connection ("tunnel:<sessionID>") so they
// survive closing the session's terminal tab.
type TunnelService struct {
	c *Core

	mu      sync.Mutex
	running map[string]*runningTunnel // key: sessionID/tunnelID
	errs    map[string]string
	last    string // fingerprint of the last emitted stats
	stopTk  chan struct{}
}

type runningTunnel struct {
	sessionID string
	f         *tunnel.Forwarder
	conn      *sshx.Conn
}

// TunnelStatus is one row in the tunnels panel.
type TunnelStatus struct {
	SessionID   string       `json:"sessionId"`
	SessionName string       `json:"sessionName"`
	Folder      string       `json:"folder"`
	Tunnel      store.Tunnel `json:"tunnel"`
	Running     bool         `json:"running"`
	Error       string       `json:"error"`
	Conns       int          `json:"conns"`
	BytesIn     int64        `json:"bytesIn"`
	BytesOut    int64        `json:"bytesOut"`
}

// NewTunnelService creates the service.
func NewTunnelService(c *Core) *TunnelService {
	s := &TunnelService{c: c, running: map[string]*runningTunnel{}, errs: map[string]string{}, stopTk: make(chan struct{})}
	c.OnShutdown(func() {
		close(s.stopTk)
		s.StopAll()
	})
	go s.ticker()
	return s
}

func tkey(sessionID, tunnelID string) string { return sessionID + "/" + tunnelID }

func poolKey(sessionID string) string { return "tunnel:" + sessionID }

func (s *TunnelService) changed() { s.c.Emit("tunnel:changed", nil) }

func (s *TunnelService) ticker() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.stopTk:
			return
		case <-t.C:
			s.mu.Lock()
			fp := ""
			for k, r := range s.running {
				st := r.f.Stats()
				fp += fmt.Sprintf("%s:%d:%d:%d;", k, st.Conns, st.BytesIn, st.BytesOut)
			}
			emit := len(s.running) > 0 && fp != s.last
			s.last = fp
			s.mu.Unlock()
			if emit {
				s.changed()
			}
		}
	}
}

// List returns every tunnel defined in saved SSH sessions with live status.
func (s *TunnelService) List() []TunnelStatus {
	var out []TunnelStatus
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.c.Store.List() {
		if sess.SSH == nil {
			continue
		}
		for _, t := range sess.SSH.Tunnels {
			row := TunnelStatus{SessionID: sess.ID, SessionName: sess.Name, Folder: sess.Folder, Tunnel: t}
			k := tkey(sess.ID, t.ID)
			if r := s.running[k]; r != nil {
				st := r.f.Stats()
				row.Running, row.Conns, row.BytesIn, row.BytesOut = true, st.Conns, st.BytesIn, st.BytesOut
			}
			row.Error = s.errs[k]
			out = append(out, row)
		}
	}
	return out
}

func (s *TunnelService) find(sessionID, tunnelID string) (*store.Session, store.Tunnel, error) {
	sess, err := s.c.Store.Get(sessionID)
	if err != nil {
		return nil, store.Tunnel{}, err
	}
	if sess.SSH == nil {
		return nil, store.Tunnel{}, errors.New("not an SSH session")
	}
	for _, t := range sess.SSH.Tunnels {
		if t.ID == tunnelID {
			return sess, t, nil
		}
	}
	return nil, store.Tunnel{}, fmt.Errorf("tunnel %s not found", tunnelID)
}

// Start starts one tunnel.
func (s *TunnelService) Start(sessionID, tunnelID string) error {
	sess, t, err := s.find(sessionID, tunnelID)
	if err != nil {
		return err
	}
	return s.start(sess, t)
}

func (s *TunnelService) start(sess *store.Session, t store.Tunnel) error {
	k := tkey(sess.ID, t.ID)
	s.mu.Lock()
	if s.running[k] != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	if err := tunnel.Validate(t); err != nil {
		s.setErr(k, err)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	conn, err := s.c.Pool.Acquire(ctx, poolKey(sess.ID), sess, "")
	if err != nil {
		err = fmt.Errorf("SSH connection: %w", err)
		s.setErr(k, err)
		return err
	}
	f, err := tunnel.Start(conn.Client, t)
	if err != nil {
		s.c.Pool.Release(poolKey(sess.ID), conn)
		s.setErr(k, err)
		return err
	}
	r := &runningTunnel{sessionID: sess.ID, f: f, conn: conn}
	s.mu.Lock()
	if s.running[k] != nil { // raced with another Start
		s.mu.Unlock()
		f.Stop()
		s.c.Pool.Release(poolKey(sess.ID), conn)
		return nil
	}
	s.running[k] = r
	delete(s.errs, k)
	s.mu.Unlock()
	go s.watch(k, r)
	s.changed()
	return nil
}

// watch handles a forwarder or its SSH connection ending on its own.
func (s *TunnelService) watch(k string, r *runningTunnel) {
	var reason error
	select {
	case <-r.f.Done():
		reason = r.f.Err()
	case <-r.conn.Done():
		reason = errors.New("SSH connection lost")
		if e := r.conn.Err(); e != nil {
			reason = fmt.Errorf("SSH connection lost: %v", e)
		}
		r.f.Stop()
	}
	s.mu.Lock()
	if s.running[k] != r { // already stopped via Stop()
		s.mu.Unlock()
		return
	}
	delete(s.running, k)
	if reason != nil {
		s.errs[k] = reason.Error()
	}
	s.mu.Unlock()
	s.c.Pool.Release(poolKey(r.sessionID), r.conn)
	s.changed()
}

func (s *TunnelService) setErr(k string, err error) {
	s.mu.Lock()
	s.errs[k] = err.Error()
	s.mu.Unlock()
	s.changed()
}

// Stop stops one tunnel.
func (s *TunnelService) Stop(sessionID, tunnelID string) error {
	k := tkey(sessionID, tunnelID)
	s.mu.Lock()
	r := s.running[k]
	delete(s.running, k)
	delete(s.errs, k)
	s.mu.Unlock()
	if r != nil {
		r.f.Stop()
		s.c.Pool.Release(poolKey(r.sessionID), r.conn)
	}
	s.changed()
	return nil
}

// StartAuto starts every tunnel of a session marked AutoStart. Errors are
// recorded per tunnel; the first one is returned.
func (s *TunnelService) StartAuto(sessionID string) error {
	sess, err := s.c.Store.Get(sessionID)
	if err != nil {
		return err
	}
	if sess.SSH == nil {
		return nil
	}
	var first error
	for _, t := range sess.SSH.Tunnels {
		if !t.AutoStart {
			continue
		}
		if err := s.start(sess, t); err != nil && first == nil {
			first = fmt.Errorf("tunnel %s: %w", tunnelLabel(t), err)
		}
	}
	return first
}

// StopAll stops every running tunnel.
func (s *TunnelService) StopAll() {
	s.mu.Lock()
	rs := s.running
	s.running = map[string]*runningTunnel{}
	s.mu.Unlock()
	for _, r := range rs {
		r.f.Stop()
		s.c.Pool.Release(poolKey(r.sessionID), r.conn)
	}
	if len(rs) > 0 {
		s.changed()
	}
}

func tunnelLabel(t store.Tunnel) string {
	if t.Name != "" {
		return t.Name
	}
	if t.Kind == "D" {
		return fmt.Sprintf("D %s", tunnel.BindAddr(t))
	}
	return fmt.Sprintf("%s %s → %s", t.Kind, tunnel.BindAddr(t), tunnel.TargetAddr(t))
}
