package app

import (
	"os"
	"path/filepath"

	"github.com/ka1hung/pinion/internal/servers"
)

// ServerService runs temporary TFTP / HTTP / FTP servers.
type ServerService struct {
	c *Core
	m *servers.Manager
}

// NewServerService creates the service.
func NewServerService(c *Core) *ServerService {
	m := servers.NewManager()
	m.OnLog = func(e servers.LogEntry) { c.Emit("server:log", e) }
	m.OnChange = func() { c.Emit("server:changed", nil) }
	m.DefaultIdle = func() int { return c.Store.Settings().ServerIdleMinutes }
	c.OnShutdown(m.StopAll)
	return &ServerService{c: c, m: m}
}

// Interfaces lists addresses servers can bind to.
func (s *ServerService) Interfaces() ([]servers.NetIface, error) { return servers.Interfaces() }

// Status returns the state of each server kind (tftp, http, ftp).
func (s *ServerService) Status() []servers.ServerStatus { return s.m.Status() }

// Start starts (or restarts) a server. An empty Root uses DefaultRoot().
func (s *ServerService) Start(cfg servers.ServerConfig) error {
	if cfg.Root == "" {
		cfg.Root = s.DefaultRoot()
	}
	return s.m.Start(cfg)
}

// Stop stops a server kind.
func (s *ServerService) Stop(kind string) error { return s.m.Stop(kind) }

// StopAll stops every server.
func (s *ServerService) StopAll() {
	for _, k := range []string{servers.KindTFTP, servers.KindHTTP, servers.KindFTP} {
		_ = s.m.Stop(k)
	}
}

// PickRoot lets the user choose a folder to serve ("" if cancelled).
func (s *ServerService) PickRoot() (string, error) { return s.c.PickFolder("Folder to serve") }

// DefaultRoot returns settings.ServerRoot, creating it if missing.
func (s *ServerService) DefaultRoot() string {
	root := s.c.Store.Settings().ServerRoot
	if root == "" {
		h, _ := os.UserHomeDir()
		root = filepath.Join(h, "Pinion", "share")
	}
	_ = os.MkdirAll(root, 0o755)
	return root
}
