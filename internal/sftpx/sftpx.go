// Package sftpx keeps one SFTP client per terminal tab, riding on the tab's
// pooled SSH connection, and provides recursive transfer helpers.
package sftpx

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/pkg/sftp"

	"github.com/ka1hung/pinion/internal/sshx"
	"github.com/ka1hung/pinion/internal/store"
)

// SessionLookup returns the session of a tab.
type SessionLookup func(tabID string) (*store.Session, error)

// Manager maps tab IDs to SFTP clients.
type Manager struct {
	pool   *sshx.Pool
	lookup SessionLookup

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	conn   *sshx.Conn
	client *sftp.Client
}

// New creates a manager.
func New(pool *sshx.Pool, lookup SessionLookup) *Manager {
	return &Manager{pool: pool, lookup: lookup, entries: map[string]*entry{}}
}

func alive(e *entry) bool {
	select {
	case <-e.conn.Done():
		return false
	default:
		return true
	}
}

// Client returns the SFTP client (and SSH connection) for a tab, creating or
// recreating it when needed.
func (m *Manager) Client(ctx context.Context, tabID string) (*sftp.Client, *sshx.Conn, error) {
	m.mu.Lock()
	e := m.entries[tabID]
	if e != nil && alive(e) {
		m.mu.Unlock()
		return e.client, e.conn, nil
	}
	if e != nil {
		delete(m.entries, tabID)
	}
	m.mu.Unlock()
	if e != nil {
		_ = e.client.Close()
		m.pool.Release(tabID, e.conn)
	}

	sess, err := m.lookup(tabID)
	if err != nil {
		return nil, nil, err
	}
	if sess.Type != store.TypeSSH || sess.SSH == nil {
		return nil, nil, errors.New("SFTP is only available for SSH sessions")
	}
	conn, err := m.pool.Acquire(ctx, tabID, sess, tabID)
	if err != nil {
		return nil, nil, err
	}
	cl, err := sftp.NewClient(conn.Client, sftp.UseConcurrentWrites(true), sftp.UseConcurrentReads(true))
	if err != nil {
		m.pool.Release(tabID, conn)
		return nil, nil, err
	}
	m.mu.Lock()
	if old := m.entries[tabID]; old != nil && alive(old) {
		// lost a race: keep the other one
		m.mu.Unlock()
		_ = cl.Close()
		m.pool.Release(tabID, conn)
		return old.client, old.conn, nil
	}
	ne := &entry{conn: conn, client: cl}
	m.entries[tabID] = ne
	m.mu.Unlock()
	return cl, conn, nil
}

// Drop closes the tab's SFTP client and releases its connection reference.
func (m *Manager) Drop(tabID string) {
	m.mu.Lock()
	e := m.entries[tabID]
	delete(m.entries, tabID)
	m.mu.Unlock()
	if e != nil {
		_ = e.client.Close()
		m.pool.Release(tabID, e.conn)
	}
}

// CloseAll drops every client.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.entries))
	for id := range m.entries {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Drop(id)
	}
}

// IsPermission reports whether err is a permission-denied error (local or SFTP).
func IsPermission(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	var se *sftp.StatusError
	if errors.As(err, &se) && se.FxCode() == sftp.ErrSSHFxPermissionDenied {
		return true
	}
	return false
}
