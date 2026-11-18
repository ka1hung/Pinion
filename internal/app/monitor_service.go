package app

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ka1hung/pinion/internal/monitor"
)

// MonitorService samples remote resource usage for SSH tabs (the bar under
// the terminal). It reuses the tab's own SSH connection, so there is no extra
// login; each sample is one short exec channel.
type MonitorService struct {
	c *Core

	mu   sync.Mutex
	prev map[string]monitorSample // key: tab ID
}

type monitorSample struct {
	raw monitor.Raw
	at  time.Time
}

const monitorTimeout = 8 * time.Second

// NewMonitorService creates the service.
func NewMonitorService(c *Core) *MonitorService {
	return &MonitorService{c: c, prev: map[string]monitorSample{}}
}

// Sample runs the probe on the tab's SSH connection and returns the stats.
// CPU and network rates need two samples, so the first call returns -1 for
// them. The UI calls this every few seconds while the bar is visible.
func (s *MonitorService) Sample(tabID string) (*monitor.Stats, error) {
	conn, err := s.c.Pool.Get(tabID)
	if err != nil {
		return nil, err
	}
	out, err := runCapped(conn.Client, monitor.Command, monitorTimeout, 512*1024)
	if err != nil && out == "" {
		return nil, err
	}
	raw := monitor.Parse(out)
	now := time.Now()

	s.mu.Lock()
	p, ok := s.prev[tabID]
	s.prev[tabID] = monitorSample{raw: raw, at: now}
	s.mu.Unlock()

	var st monitor.Stats
	if ok {
		st = monitor.Compute(&p.raw, raw, now.Sub(p.at).Seconds())
	} else {
		st = monitor.Compute(nil, raw, 0)
	}
	return &st, nil
}

// Forget drops the stored previous sample of a tab (bar hidden or tab closed).
func (s *MonitorService) Forget(tabID string) {
	s.mu.Lock()
	delete(s.prev, tabID)
	s.mu.Unlock()
}

// runCapped runs cmd in a new session and returns at most limit bytes of
// stdout. A non-zero exit status is not an error when there is output: one
// failing probe (say, no df) must not hide the rest.
func runCapped(client *ssh.Client, cmd string, timeout time.Duration, limit int) (string, error) {
	sess, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	buf := &cappedBuffer{limit: limit}
	sess.Stdout = buf
	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case err = <-done:
	case <-time.After(timeout):
		_ = sess.Close()
		return "", fmt.Errorf("monitor: no reply within %s", timeout)
	}
	var exitErr *ssh.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return buf.String(), err
	}
	return buf.String(), nil
}

type cappedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.Len(); room > 0 {
		if len(p) > room {
			b.Buffer.Write(p[:room])
		} else {
			b.Buffer.Write(p)
		}
	}
	return len(p), nil // keep draining so the remote side never blocks
}
