// Package servers runs temporary TFTP / HTTP / FTP servers for firmware
// flashing and quick file exchange. Servers are off by default, bind only to
// the chosen interface, auto-stop when idle, and TFTP is read-only unless
// explicitly made writable.
package servers

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Kinds.
const (
	KindTFTP = "tftp"
	KindHTTP = "http"
	KindFTP  = "ftp"
)

// ServerConfig configures one server.
type ServerConfig struct {
	Kind         string `json:"kind"`
	Root         string `json:"root"`
	BindIP       string `json:"bindIp"` // "" or "0.0.0.0" = all interfaces
	Port         int    `json:"port"`   // 0 = default (69 / 8080 / 2121)
	Writable     bool   `json:"writable"`
	User         string `json:"user"`     // ftp/http; empty = anonymous
	Password     string `json:"password"` // ftp/http
	IdleMinutes  int    `json:"idleMinutes"`
	PassivePorts string `json:"passivePorts"` // ftp, e.g. "50000-50100"
}

// ServerStatus is reported to the UI.
type ServerStatus struct {
	Kind         string       `json:"kind"`
	Running      bool         `json:"running"`
	Config       ServerConfig `json:"config"`
	URL          string       `json:"url"`
	StartedAt    int64        `json:"startedAt"`
	LastActivity int64        `json:"lastActivity"`
	Error        string       `json:"error"`
}

// LogEntry is one request / transfer.
type LogEntry struct {
	Kind    string `json:"kind"`
	Time    int64  `json:"time"` // unix ms
	Remote  string `json:"remote"`
	Action  string `json:"action"` // read | write | list | login | error
	File    string `json:"file"`
	Bytes   int64  `json:"bytes"`
	Message string `json:"message"`
}

// NetIface is a bindable address.
type NetIface struct {
	Name  string `json:"name"`
	IP    string `json:"ip"`
	Label string `json:"label"`
}

// DefaultPort returns the default port of a kind.
func DefaultPort(kind string) int {
	switch kind {
	case KindTFTP:
		return 69
	case KindHTTP:
		return 8080
	case KindFTP:
		return 2121
	}
	return 0
}

type stopper interface{ stop() error }

type instance struct {
	cfg       ServerConfig
	srv       stopper
	startedAt time.Time
	last      atomic.Int64
	addr      string
}

// Manager runs at most one server per kind.
type Manager struct {
	OnLog    func(LogEntry)
	OnChange func()
	// DefaultIdle is used when ServerConfig.IdleMinutes is 0.
	DefaultIdle func() int

	mu   sync.Mutex
	inst map[string]*instance
	errs map[string]string
	quit chan struct{}
	once sync.Once
}

// NewManager creates a manager and starts its idle checker.
func NewManager() *Manager {
	m := &Manager{inst: map[string]*instance{}, errs: map[string]string{}, quit: make(chan struct{})}
	go m.idleLoop()
	return m
}

func (m *Manager) changed() {
	if m.OnChange != nil {
		m.OnChange()
	}
}

func (m *Manager) log(e LogEntry) {
	if e.Time == 0 {
		e.Time = time.Now().UnixMilli()
	}
	m.mu.Lock()
	if in := m.inst[e.Kind]; in != nil {
		in.last.Store(time.Now().UnixMilli())
	}
	m.mu.Unlock()
	if m.OnLog != nil {
		m.OnLog(e)
	}
}

func (m *Manager) touch(kind string) {
	m.mu.Lock()
	if in := m.inst[kind]; in != nil {
		in.last.Store(time.Now().UnixMilli())
	}
	m.mu.Unlock()
}

func (m *Manager) idleMinutes(cfg ServerConfig) int {
	if cfg.IdleMinutes > 0 {
		return cfg.IdleMinutes
	}
	if m.DefaultIdle != nil {
		return m.DefaultIdle()
	}
	return 30
}

func (m *Manager) idleLoop() {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.quit:
			return
		case <-t.C:
			m.checkIdle(time.Now())
		}
	}
}

func (m *Manager) checkIdle(now time.Time) {
	var expired []string
	m.mu.Lock()
	for k, in := range m.inst {
		idle := m.idleMinutes(in.cfg)
		if idle <= 0 {
			continue
		}
		if now.Sub(time.UnixMilli(in.last.Load())) >= time.Duration(idle)*time.Minute {
			expired = append(expired, k)
		}
	}
	m.mu.Unlock()
	for _, k := range expired {
		_ = m.Stop(k)
		m.mu.Lock()
		m.errs[k] = "stopped after being idle"
		m.mu.Unlock()
		m.log(LogEntry{Kind: k, Action: "error", Message: "auto-stopped (idle timeout)"})
		m.changed()
	}
}

// Start starts (or restarts) a server.
func (m *Manager) Start(cfg ServerConfig) error {
	cfg.Kind = strings.ToLower(cfg.Kind)
	if cfg.Port == 0 {
		cfg.Port = DefaultPort(cfg.Kind)
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return errors.New("invalid port")
	}
	if cfg.BindIP == "0.0.0.0" {
		cfg.BindIP = ""
	}
	if cfg.BindIP != "" && net.ParseIP(cfg.BindIP) == nil {
		return fmt.Errorf("invalid bind address %q", cfg.BindIP)
	}
	root, err := filepath.Abs(cfg.Root)
	if err != nil || cfg.Root == "" {
		return errors.New("choose a root folder")
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return fmt.Errorf("root folder %s does not exist", root)
	}
	cfg.Root = root
	_ = m.Stop(cfg.Kind)

	addr := net.JoinHostPort(cfg.BindIP, strconv.Itoa(cfg.Port))
	var srv stopper
	switch cfg.Kind {
	case KindTFTP:
		srv, err = startTFTP(m, cfg, addr)
	case KindHTTP:
		srv, err = startHTTP(m, cfg, addr)
	case KindFTP:
		srv, err = startFTP(m, cfg, addr)
	default:
		return fmt.Errorf("unknown server kind %q", cfg.Kind)
	}
	if err != nil {
		err = FriendlyBindError(cfg.Kind, addr, cfg.Port, err)
		m.mu.Lock()
		m.errs[cfg.Kind] = err.Error()
		m.mu.Unlock()
		m.changed()
		return err
	}
	in := &instance{cfg: cfg, srv: srv, startedAt: time.Now(), addr: addr}
	in.last.Store(time.Now().UnixMilli())
	m.mu.Lock()
	m.inst[cfg.Kind] = in
	delete(m.errs, cfg.Kind)
	m.mu.Unlock()
	m.log(LogEntry{Kind: cfg.Kind, Action: "login", Message: "started on " + addr + " serving " + root})
	m.changed()
	return nil
}

// Stop stops a server (no error if not running).
func (m *Manager) Stop(kind string) error {
	m.mu.Lock()
	in := m.inst[kind]
	delete(m.inst, kind)
	delete(m.errs, kind)
	m.mu.Unlock()
	if in == nil {
		return nil
	}
	err := in.srv.stop()
	m.changed()
	return err
}

// StopAll stops everything and the idle checker.
func (m *Manager) StopAll() {
	m.once.Do(func() { close(m.quit) })
	for _, k := range []string{KindTFTP, KindHTTP, KindFTP} {
		_ = m.Stop(k)
	}
}

// Status returns one row per kind.
func (m *Manager) Status() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ServerStatus
	for _, k := range []string{KindTFTP, KindHTTP, KindFTP} {
		st := ServerStatus{Kind: k, Error: m.errs[k]}
		if in := m.inst[k]; in != nil {
			st.Running = true
			st.Config = in.cfg
			st.Config.Password = ""
			st.StartedAt = in.startedAt.UnixMilli()
			st.LastActivity = in.last.Load()
			st.URL = URL(in.cfg)
		}
		out = append(out, st)
	}
	return out
}

// URL builds a display URL for a running server.
func URL(cfg ServerConfig) string {
	host := cfg.BindIP
	if host == "" {
		host = firstIPv4()
		if host == "" {
			host = "127.0.0.1"
		}
	}
	return fmt.Sprintf("%s://%s/", cfg.Kind, net.JoinHostPort(host, strconv.Itoa(cfg.Port)))
}

func firstIPv4() string {
	ifs, _ := Interfaces()
	for _, i := range ifs {
		if i.IP != "0.0.0.0" && i.IP != "127.0.0.1" {
			return i.IP
		}
	}
	return ""
}

// Interfaces lists bindable addresses: IPv4 non-loopback first, then all
// interfaces (0.0.0.0) and loopback.
func Interfaces() ([]NetIface, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []NetIface
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipn.IP.To4()
			if ip4 == nil || ip4.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, NetIface{Name: ifc.Name, IP: ip4.String(), Label: fmt.Sprintf("%s — %s", ip4, ifc.Name)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	out = append(out,
		NetIface{Name: "all", IP: "0.0.0.0", Label: "All interfaces (0.0.0.0)"},
		NetIface{Name: "loopback", IP: "127.0.0.1", Label: "Loopback (127.0.0.1)"},
	)
	return out, nil
}

// FriendlyBindError explains why a server could not listen.
func FriendlyBindError(kind, addr string, port int, err error) error {
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case errors.Is(err, syscall.EADDRINUSE) || strings.Contains(low, "address already in use") || strings.Contains(msg, "Only one usage of each socket address"):
		return fmt.Errorf("%s: port %d is already in use (another TFTP/HTTP/FTP server running?)", strings.ToUpper(kind), port)
	case errors.Is(err, syscall.EACCES) || strings.Contains(low, "permission denied") || strings.Contains(low, "forbidden by its access permissions"):
		switch runtime.GOOS {
		case "linux":
			return fmt.Errorf("%s: permission denied binding %s. Ports below 1024 need root on Linux: run `sudo setcap cap_net_bind_service=+ep <path to Pinion>` once, or use a port above 1024", strings.ToUpper(kind), addr)
		case "windows":
			return fmt.Errorf("%s: permission denied binding %s. The port may be reserved or blocked; allow Pinion through Windows Defender Firewall or choose another port", strings.ToUpper(kind), addr)
		default:
			return fmt.Errorf("%s: permission denied binding %s; try another port", strings.ToUpper(kind), addr)
		}
	case strings.Contains(low, "cannot assign requested address") || strings.Contains(low, "requested address is not valid"):
		return fmt.Errorf("%s: %s is not an address of this computer (interface down or IP changed?)", strings.ToUpper(kind), addr)
	}
	return fmt.Errorf("%s: cannot listen on %s: %w", strings.ToUpper(kind), addr, err)
}

// resolvePath maps a client-supplied path to a file under root, rejecting
// anything that would escape root (including through symlinks).
func resolvePath(root, name string) (string, error) {
	name = strings.ReplaceAll(name, `\`, "/")
	clean := filepath.FromSlash(strings.TrimPrefix(filepathClean("/"+name), "/"))
	p := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("path outside root")
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		rr, err := filepath.EvalSymlinks(root)
		if err != nil {
			rr = root
		}
		rel, err := filepath.Rel(rr, real)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", errors.New("path outside root")
		}
	}
	return p, nil
}

func filepathClean(p string) string {
	// path.Clean semantics on forward slashes, independent of OS
	parts := strings.Split(p, "/")
	var out []string
	for _, s := range parts {
		switch s {
		case "", ".":
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
		default:
			out = append(out, s)
		}
	}
	return "/" + strings.Join(out, "/")
}
