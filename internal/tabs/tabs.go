// Package tabs owns open terminal tabs: it opens a Transport for a session,
// pumps its output into a stream channel (and optional session log), routes
// UI input back, and handles disconnect / reconnect.
package tabs

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ka1hung/pinion/internal/charset"
	"github.com/ka1hung/pinion/internal/store"
	"github.com/ka1hung/pinion/internal/stream"
	"github.com/ka1hung/pinion/internal/termlog"
	"github.com/ka1hung/pinion/internal/transport"
)

// States.
const (
	StateConnecting   = "connecting"
	StateConnected    = "connected"
	StateDisconnected = "disconnected"
	StateReconnecting = "reconnecting"
)

// Factory creates a transport for a session.
type Factory func(sess *store.Session, tabID string, cols, rows int) (transport.Transport, error)

// Emitter sends UI events.
type Emitter func(name string, data any)

// Info describes a tab for the UI.
type Info struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	State     string `json:"state"`
	Message   string `json:"message"`
	Describe  string `json:"describe"`
	LogPath   string `json:"logPath"`
}

// Manager holds all tabs.
type Manager struct {
	hub      *stream.Hub
	factory  Factory
	emit     Emitter
	settings func() store.Settings
	onClose  []func(tabID string)

	seq  atomic.Uint64
	mu   sync.RWMutex
	tabs map[string]*Tab
}

// NewManager creates a manager.
func NewManager(hub *stream.Hub, f Factory, emit Emitter, settings func() store.Settings) *Manager {
	return &Manager{hub: hub, factory: f, emit: emit, settings: settings, tabs: map[string]*Tab{}}
}

// OnClose registers a hook called after a tab is closed (SFTP/editor cleanup).
func (m *Manager) OnClose(f func(tabID string)) { m.onClose = append(m.onClose, f) }

// Tab is one open terminal.
type Tab struct {
	m     *Manager
	id    string
	sess  *store.Session
	ch    *stream.Channel
	dec   *charset.StreamDecoder
	decMu sync.Mutex

	mu         sync.Mutex
	tr         transport.Transport
	state      string
	message    string
	cols, rows int
	logger     *termlog.Logger
	ctx        context.Context
	cancel     context.CancelFunc
	gen        int // connection generation
	userClosed bool
}

// Session returns the tab's session snapshot.
func (t *Tab) Session() *store.Session { return t.sess }

// ID returns the tab ID.
func (t *Tab) ID() string { return t.id }

// Transport returns the current transport (may be nil while connecting).
func (t *Tab) Transport() transport.Transport {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tr
}

// Open creates a tab and starts connecting in the background.
func (m *Manager) Open(sess *store.Session, cols, rows int) (*Info, error) {
	if sess == nil {
		return nil, errors.New("no session")
	}
	sess.Normalize()
	id := "t" + strconv.FormatUint(m.seq.Add(1), 10)
	ctx, cancel := context.WithCancel(context.Background())
	t := &Tab{m: m, id: id, sess: sess, cols: cols, rows: rows, ctx: ctx, cancel: cancel, state: StateConnecting}
	t.dec = charset.NewStreamDecoder(sess.Charset)
	t.ch = m.hub.Open(id, t)
	m.mu.Lock()
	m.tabs[id] = t
	m.mu.Unlock()
	if sess.LogToFile {
		if _, err := t.startLog(); err != nil {
			t.notice("log: " + err.Error())
		}
	}
	go t.connectLoop(false)
	return t.info(), nil
}

// Get returns a tab.
func (m *Manager) Get(id string) (*Tab, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t := m.tabs[id]
	if t == nil {
		return nil, fmt.Errorf("tab %s not found", id)
	}
	return t, nil
}

// List returns all tabs.
func (m *Manager) List() []*Info {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Info, 0, len(m.tabs))
	for _, t := range m.tabs {
		out = append(out, t.info())
	}
	return out
}

// Close closes a tab.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	t := m.tabs[id]
	delete(m.tabs, id)
	m.mu.Unlock()
	if t == nil {
		return nil
	}
	t.mu.Lock()
	t.userClosed = true
	tr := t.tr
	t.tr = nil
	lg := t.logger
	t.logger = nil
	t.mu.Unlock()
	t.cancel()
	if tr != nil {
		_ = tr.Close()
	}
	if lg != nil {
		lg.Note("closed")
		_ = lg.Close()
	}
	m.hub.CloseChannel(id)
	for _, f := range m.onClose {
		f(id)
	}
	return nil
}

// CloseAll closes every tab.
func (m *Manager) CloseAll() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.tabs))
	for id := range m.tabs {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		_ = m.Close(id)
	}
}

// Reconnect drops the current transport and connects again.
func (m *Manager) Reconnect(id string) error {
	t, err := m.Get(id)
	if err != nil {
		return err
	}
	t.mu.Lock()
	if t.state == StateConnecting || t.state == StateReconnecting {
		t.mu.Unlock()
		return nil
	}
	tr := t.tr
	t.tr = nil
	t.gen++
	t.mu.Unlock()
	if tr != nil {
		_ = tr.Close()
	}
	go t.connectLoop(false)
	return nil
}

// Broadcast writes the same input to several tabs (MultiExec).
func (m *Manager) Broadcast(ids []string, data []byte) {
	for _, id := range ids {
		if t, err := m.Get(id); err == nil {
			t.Input(data)
		}
	}
}

func (t *Tab) info() *Info {
	t.mu.Lock()
	defer t.mu.Unlock()
	in := &Info{ID: t.id, SessionID: t.sess.ID, Title: t.sess.Name, Type: t.sess.Type, State: t.state, Message: t.message}
	if d, ok := t.tr.(transport.Info); ok && t.tr != nil {
		in.Describe = d.Describe()
	}
	if t.logger != nil {
		in.LogPath = t.logger.FilePath()
	}
	return in
}

func (t *Tab) setState(state, msg string) {
	t.mu.Lock()
	t.state, t.message = state, msg
	t.mu.Unlock()
	if t.m.emit != nil {
		t.m.emit("tab:state", t.info())
	}
}

// notice shows an out-of-band line in the terminal.
func (t *Tab) notice(msg string) {
	if t.m.emit != nil {
		t.m.emit("tab:notice", map[string]string{"id": t.id, "text": msg})
		// transports may change their description (e.g. client count)
		t.m.emit("tab:state", t.info())
	}
}

func (t *Tab) writeStatus(msg string) {
	_, _ = t.ch.Write([]byte("\r\n\x1b[2m\x1b[33m[Pinion] " + msg + "\x1b[0m\r\n"))
}

func (t *Tab) autoReconnect() bool {
	return t.sess.SSH != nil && t.sess.SSH.AutoReconnect
}

var backoff = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 20 * time.Second, 30 * time.Second}

func (t *Tab) connectLoop(isRetry bool) {
	attempt := 0
	for {
		t.mu.Lock()
		if t.userClosed {
			t.mu.Unlock()
			return
		}
		cols, rows := t.cols, t.rows
		gen := t.gen
		t.mu.Unlock()
		if isRetry {
			t.setState(StateReconnecting, fmt.Sprintf("attempt %d", attempt+1))
		} else {
			t.setState(StateConnecting, "")
		}
		tr, err := t.m.factory(t.sess, t.id, cols, rows)
		if err == nil {
			ctx, cancel := context.WithTimeout(t.ctx, 60*time.Second)
			err = tr.Open(ctx)
			cancel()
		}
		if err != nil {
			if t.ctx.Err() != nil {
				return
			}
			if isRetry && attempt < 20 {
				d := backoff[min(attempt, len(backoff)-1)]
				t.writeStatus(fmt.Sprintf("Reconnect failed: %v — retrying in %s", err, d))
				attempt++
				select {
				case <-time.After(d):
					continue
				case <-t.ctx.Done():
					return
				}
			}
			t.writeStatus("Connection failed: " + err.Error() + "  (press R to retry)")
			t.setState(StateDisconnected, err.Error())
			return
		}
		t.mu.Lock()
		if t.userClosed || gen != t.gen {
			t.mu.Unlock()
			_ = tr.Close()
			return
		}
		t.tr = tr
		lg := t.logger
		// The UI usually reports its real size while we were still dialing;
		// apply the latest size now, otherwise the remote (or ConPTY) keeps
		// the initial size and cursor positioning is off.
		curCols, curRows := t.cols, t.rows
		t.mu.Unlock()
		if curCols > 0 && curRows > 0 && (curCols != cols || curRows != rows) {
			_ = tr.Resize(curCols, curRows)
		}
		if lg != nil {
			lg.Note("connected")
		}
		t.setState(StateConnected, "")
		if n, ok := tr.(transport.Notifier); ok {
			go func() {
				for msg := range n.Notices() {
					t.notice(msg)
				}
			}()
		}
		err = t.pump(tr)
		t.mu.Lock()
		closed := t.userClosed
		stale := gen != t.gen
		if t.tr == tr {
			t.tr = nil
		}
		lg = t.logger
		t.mu.Unlock()
		_ = tr.Close()
		if closed || stale {
			return
		}
		msg := "Session closed"
		if err != nil && !transport.IsEOF(err) {
			msg = "Connection lost: " + err.Error()
		}
		if lg != nil {
			lg.Note(msg)
		}
		if t.autoReconnect() && err != nil && !transport.IsEOF(err) {
			t.writeStatus(msg + " — reconnecting…")
			isRetry, attempt = true, 0
			select {
			case <-time.After(time.Second):
				continue
			case <-t.ctx.Done():
				return
			}
		}
		t.writeStatus(msg + "  (press R to reconnect)")
		t.setState(StateDisconnected, msg)
		return
	}
}

func (t *Tab) pump(tr transport.Transport) error {
	buf := make([]byte, 32*1024)
	for {
		// backpressure: stop reading while the UI is behind
		t.ch.Wait(t.ctx)
		n, err := tr.Read(buf)
		if n > 0 {
			t.decMu.Lock()
			out := t.dec.Decode(buf[:n])
			if t.dec == nil {
				out = append([]byte(nil), out...)
			}
			t.decMu.Unlock()
			_, _ = t.ch.Write(out)
			t.mu.Lock()
			lg := t.logger
			t.mu.Unlock()
			if lg != nil {
				_, _ = lg.Write(out)
			}
		}
		if err != nil {
			return err
		}
	}
}

// Input implements stream.Handler: keystrokes from the UI.
func (t *Tab) Input(p []byte) {
	t.mu.Lock()
	tr, state := t.tr, t.state
	t.mu.Unlock()
	if tr == nil {
		if state == StateDisconnected && len(p) == 1 && (p[0] == 'r' || p[0] == 'R') {
			_ = t.m.Reconnect(t.id)
		}
		return
	}
	data := p
	if s := t.sess.Serial; s != nil {
		data = mapEnter(data, s.SendLineEnd)
		if s.LocalEcho {
			_, _ = t.ch.Write(echoText(p))
		}
	}
	if n := t.sess.Net; n != nil && n.LocalEcho {
		_, _ = t.ch.Write(echoText(p))
	}
	data = charset.Encode(t.sess.Charset, data)
	_, _ = tr.Write(data)
}

// Send writes raw bytes (already encoded) without Enter mapping.
func (t *Tab) Send(p []byte) error {
	t.mu.Lock()
	tr := t.tr
	t.mu.Unlock()
	if tr == nil {
		return errors.New("not connected")
	}
	_, err := tr.Write(p)
	return err
}

func mapEnter(p []byte, mode string) []byte {
	if mode == "" || mode == "cr" || !strings.Contains(string(p), "\r") {
		return p
	}
	rep := "\n"
	if mode == "crlf" {
		rep = "\r\n"
	}
	return []byte(strings.ReplaceAll(string(p), "\r", rep))
}

func echoText(p []byte) []byte {
	return []byte(strings.ReplaceAll(string(p), "\r", "\r\n"))
}

// Resize implements stream.Handler.
func (t *Tab) Resize(cols, rows int) {
	t.mu.Lock()
	t.cols, t.rows = cols, rows
	tr := t.tr
	t.mu.Unlock()
	if tr != nil {
		_ = tr.Resize(cols, rows)
	}
}

func (t *Tab) startLog() (string, error) {
	st := t.m.settings()
	lg, err := termlog.Open(termlog.Options{
		Dir: st.LogDir, Folder: t.sess.Folder, Name: t.sess.Name,
		StripANSI: st.LogStripANSI, Timestamps: st.LogTimestamps,
	})
	if err != nil {
		return "", err
	}
	t.mu.Lock()
	old := t.logger
	t.logger = lg
	t.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	return lg.FilePath(), nil
}

// SetLogging turns the session log on/off for a tab; returns the log path.
func (m *Manager) SetLogging(id string, on bool) (string, error) {
	t, err := m.Get(id)
	if err != nil {
		return "", err
	}
	if on {
		p, err := t.startLog()
		if err == nil && t.m.emit != nil {
			t.m.emit("tab:state", t.info())
		}
		return p, err
	}
	t.mu.Lock()
	lg := t.logger
	t.logger = nil
	t.mu.Unlock()
	if lg != nil {
		_ = lg.Close()
	}
	if t.m.emit != nil {
		t.m.emit("tab:state", t.info())
	}
	return "", nil
}

// ClearScrollback drops the replay buffer.
func (m *Manager) ClearScrollback(id string) {
	if c := m.hub.Get(id); c != nil {
		c.ClearReplay()
	}
}
