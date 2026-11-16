package servers

import (
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
)

type ftpServer struct {
	s *ftpserver.FtpServer
	d *ftpDriver
}

func (f *ftpServer) stop() error {
	err := f.s.Stop()
	f.d.clients.Range(func(k, _ any) bool {
		_ = k.(ftpserver.ClientContext).Close()
		return true
	})
	return err
}

type ftpDriver struct {
	m       *Manager
	cfg     ServerConfig
	ln      net.Listener
	pasv    ftpserver.PasvPortGetter
	baseFs  afero.Fs
	clients sync.Map
}

// ParsePortRange parses "50000-50100".
func ParsePortRange(s string) (*ftpserver.PortRange, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	a, b, ok := strings.Cut(s, "-")
	lo, err1 := strconv.Atoi(strings.TrimSpace(a))
	hi, err2 := strconv.Atoi(strings.TrimSpace(b))
	if !ok || err1 != nil || err2 != nil || lo < 1024 || hi > 65535 || lo > hi {
		return nil, fmt.Errorf("invalid passive port range %q (use e.g. 50000-50100)", s)
	}
	return &ftpserver.PortRange{Start: lo, End: hi}, nil
}

func startFTP(m *Manager, cfg ServerConfig, addr string) (stopper, error) {
	pr, err := ParsePortRange(cfg.PassivePorts)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	var fs afero.Fs = afero.NewBasePathFs(afero.NewOsFs(), cfg.Root)
	if !cfg.Writable {
		fs = afero.NewReadOnlyFs(fs)
	}
	d := &ftpDriver{m: m, cfg: cfg, ln: ln, baseFs: fs}
	if pr != nil {
		d.pasv = *pr
	}
	s := ftpserver.NewFtpServer(d)
	if err := s.Listen(); err != nil {
		_ = ln.Close()
		return nil, err
	}
	go func() { _ = s.Serve() }()
	return &ftpServer{s: s, d: d}, nil
}

func (d *ftpDriver) GetSettings() (*ftpserver.Settings, error) {
	st := &ftpserver.Settings{
		Listener:                 d.ln,
		Banner:                   "Pinion FTP server",
		PassiveTransferPortRange: d.pasv,
		IdleTimeout:              900,
		ConnectionTimeout:        30,
	}
	if d.cfg.BindIP != "" {
		st.PublicHost = d.cfg.BindIP
	}
	return st, nil
}

func (d *ftpDriver) ClientConnected(cc ftpserver.ClientContext) (string, error) {
	d.m.touch(KindFTP)
	d.clients.Store(cc, struct{}{})
	return "Pinion FTP server ready", nil
}

func (d *ftpDriver) ClientDisconnected(cc ftpserver.ClientContext) { d.clients.Delete(cc) }

func (d *ftpDriver) AuthUser(cc ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	remote := cc.RemoteAddr().String()
	if d.cfg.User != "" {
		if subtle.ConstantTimeCompare([]byte(user), []byte(d.cfg.User)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(d.cfg.Password)) != 1 {
			d.m.log(LogEntry{Kind: KindFTP, Remote: remote, Action: "error", Message: "login failed for " + user})
			return nil, errors.New("invalid credentials")
		}
	}
	d.m.log(LogEntry{Kind: KindFTP, Remote: remote, Action: "login", Message: "user " + user})
	return &logFs{Fs: d.baseFs, m: d.m, remote: remote}, nil
}

func (d *ftpDriver) GetTLSConfig() (*tls.Config, error) { return nil, errors.New("TLS is not enabled") }

// logFs wraps the client filesystem to log transfers and track activity.
type logFs struct {
	afero.Fs
	m      *Manager
	remote string
}

func (l *logFs) Open(name string) (afero.File, error) {
	return l.OpenFile(name, os.O_RDONLY, 0)
}

func (l *logFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	l.m.touch(KindFTP)
	f, err := l.Fs.OpenFile(name, flag, perm)
	if err != nil {
		if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE) != 0 {
			l.m.log(LogEntry{Kind: KindFTP, Remote: l.remote, Action: "error", File: name, Message: err.Error()})
		}
		return nil, err
	}
	if st, err := f.Stat(); err == nil && st.IsDir() {
		l.m.log(LogEntry{Kind: KindFTP, Remote: l.remote, Action: "list", File: name})
		return f, nil
	}
	write := flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_APPEND) != 0
	return &logFile{File: f, l: l, name: name, write: write}, nil
}

type logFile struct {
	afero.File
	l      *logFs
	name   string
	write  bool
	n      atomic.Int64
	closed atomic.Bool
}

func (f *logFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	f.n.Add(int64(n))
	return n, err
}

func (f *logFile) ReadAt(p []byte, off int64) (int, error) {
	n, err := f.File.ReadAt(p, off)
	f.n.Add(int64(n))
	return n, err
}

func (f *logFile) Write(p []byte) (int, error) {
	n, err := f.File.Write(p)
	f.n.Add(int64(n))
	if n > 0 {
		f.l.m.touch(KindFTP)
	}
	return n, err
}

func (f *logFile) WriteAt(p []byte, off int64) (int, error) {
	n, err := f.File.WriteAt(p, off)
	f.n.Add(int64(n))
	return n, err
}

func (f *logFile) Close() error {
	err := f.File.Close()
	if f.closed.CompareAndSwap(false, true) {
		action := "read"
		if f.write {
			action = "write"
		}
		if f.n.Load() > 0 || f.write {
			f.l.m.log(LogEntry{Kind: KindFTP, Remote: f.l.remote, Action: action, File: f.name, Bytes: f.n.Load()})
		}
	}
	return err
}
