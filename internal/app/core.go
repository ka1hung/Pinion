// Package app contains the Wails-bound services. Control calls go through
// these bindings; terminal byte streams go through the local WebSocket hub.
package app

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/ka1hung/pinion/internal/prompt"
	"github.com/ka1hung/pinion/internal/secret"
	"github.com/ka1hung/pinion/internal/sshx"
	"github.com/ka1hung/pinion/internal/store"
	"github.com/ka1hung/pinion/internal/stream"
	"github.com/ka1hung/pinion/internal/tabs"
	"github.com/ka1hung/pinion/internal/transport"
)

// TransportBuilder creates a transport for a session type.
type TransportBuilder func(c *Core, sess *store.Session, tabID string, cols, rows int) (transport.Transport, error)

// Core holds shared backend state used by every service.
type Core struct {
	App     *application.App
	Store   *store.Store
	Secrets *secret.Store
	Prompt  *prompt.Broker
	Dialer  *sshx.Dialer
	Pool    *sshx.Pool
	Hub     *stream.Hub
	Tabs    *tabs.Manager
	Win     *application.WebviewWindow // main window

	mu         sync.RWMutex
	transports map[string]TransportBuilder
	shutdown   []func()
}

// NewCore wires everything up. app may be set later via SetApp.
func NewCore(configDir string) (*Core, error) {
	st, err := store.Open(configDir)
	if err != nil {
		return nil, err
	}
	hub, err := stream.NewHub()
	if err != nil {
		return nil, err
	}
	c := &Core{Store: st, Hub: hub, transports: map[string]TransportBuilder{}}
	c.Secrets = secret.New(filepath.Join(configDir, "vault.json"))
	c.Prompt = prompt.NewBroker(c.Emit)
	c.Dialer = sshx.NewDialer(st, c.Secrets, c.Prompt)
	c.Pool = sshx.NewPool(c.Dialer)
	c.Tabs = tabs.NewManager(hub, c.buildTransport, c.Emit, st.Settings)
	registerBuiltinTransports(c)
	return c, nil
}

// SetApp attaches the Wails application (for events and dialogs).
func (c *Core) SetApp(a *application.App) { c.App = a }

// Emit sends an event to the frontend.
func (c *Core) Emit(name string, data any) {
	if c.App != nil {
		c.App.Event.Emit(name, data)
	}
}

// RegisterTransport adds a builder for a session type.
func (c *Core) RegisterTransport(typ string, b TransportBuilder) {
	c.mu.Lock()
	c.transports[typ] = b
	c.mu.Unlock()
}

// OnShutdown registers cleanup run when the app exits.
func (c *Core) OnShutdown(f func()) {
	c.mu.Lock()
	c.shutdown = append(c.shutdown, f)
	c.mu.Unlock()
}

// Shutdown releases all resources.
func (c *Core) Shutdown() {
	c.mu.RLock()
	fs := append([]func(){}, c.shutdown...)
	c.mu.RUnlock()
	for i := len(fs) - 1; i >= 0; i-- {
		fs[i]()
	}
	c.Tabs.CloseAll()
	c.Pool.CloseAll()
	_ = c.Hub.Close()
}

func (c *Core) buildTransport(sess *store.Session, tabID string, cols, rows int) (transport.Transport, error) {
	c.mu.RLock()
	b := c.transports[sess.Type]
	c.mu.RUnlock()
	if b == nil {
		return nil, fmt.Errorf("unsupported session type %q", sess.Type)
	}
	return b(c, sess, tabID, cols, rows)
}

// MainWindow returns the first window (for attaching dialogs).
func (c *Core) MainWindow() application.Window {
	if c.App == nil {
		return nil
	}
	if w, ok := c.App.Window.GetByName("main"); ok {
		return w
	}
	return c.App.Window.Current()
}

func registerBuiltinTransports(c *Core) {
	c.RegisterTransport(store.TypeSSH, func(c *Core, s *store.Session, tabID string, cols, rows int) (transport.Transport, error) {
		t := &transport.SSH{Pool: c.Pool, Session: s, Key: tabID, TabID: tabID, Cols: cols, Rows: rows}
		if c.Store.Settings().SSHCompletionIgnoreCase {
			// leading space keeps it out of bash history; errors hidden on non-bash shells
			t.Init = " bind 'set completion-ignore-case on' 2>/dev/null\n"
		}
		return t, nil
	})
	c.RegisterTransport(store.TypeLocal, func(c *Core, s *store.Session, tabID string, cols, rows int) (transport.Transport, error) {
		return &transport.Local{Config: s.Local, Cols: cols, Rows: rows}, nil
	})
	c.RegisterTransport(store.TypeSerial, func(c *Core, s *store.Session, tabID string, cols, rows int) (transport.Transport, error) {
		return &transport.Serial{Config: s.Serial}, nil
	})
	c.RegisterTransport(store.TypeTelnet, func(c *Core, s *store.Session, tabID string, cols, rows int) (transport.Transport, error) {
		return &transport.Telnet{Host: s.Telnet.Host, Port: s.Telnet.Port, Cols: cols, Rows: rows}, nil
	})
	c.RegisterTransport(store.TypeTCP, func(c *Core, s *store.Session, tabID string, cols, rows int) (transport.Transport, error) {
		return &transport.NetSock{Proto: "tcp", Config: s.Net}, nil
	})
	c.RegisterTransport(store.TypeUDP, func(c *Core, s *store.Session, tabID string, cols, rows int) (transport.Transport, error) {
		return &transport.NetSock{Proto: "udp", Config: s.Net}, nil
	})
}
