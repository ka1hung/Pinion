package servers

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/pin/tftp/v3"
)

type tftpServer struct {
	s    *tftp.Server
	conn net.PacketConn
	done chan struct{}
}

func (t *tftpServer) stop() error {
	t.s.Shutdown()
	_ = t.conn.Close()
	select {
	case <-t.done:
	case <-time.After(3 * time.Second):
	}
	return nil
}

func remoteOf(v any) string {
	type ra interface{ RemoteAddr() net.UDPAddr }
	if r, ok := v.(ra); ok {
		a := r.RemoteAddr()
		return a.String()
	}
	return ""
}

func startTFTP(m *Manager, cfg ServerConfig, addr string) (stopper, error) {
	read := func(filename string, rf io.ReaderFrom) error {
		remote := remoteOf(rf)
		p, err := resolvePath(cfg.Root, filename)
		if err != nil {
			m.log(LogEntry{Kind: KindTFTP, Remote: remote, Action: "error", File: filename, Message: err.Error()})
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			m.log(LogEntry{Kind: KindTFTP, Remote: remote, Action: "error", File: filename, Message: "not found"})
			return errors.New("file not found")
		}
		defer f.Close()
		if st, err := f.Stat(); err == nil && st.IsDir() {
			return errors.New("is a directory")
		}
		m.touch(KindTFTP)
		n, err := rf.ReadFrom(f)
		e := LogEntry{Kind: KindTFTP, Remote: remote, Action: "read", File: filename, Bytes: n}
		if err != nil {
			e.Action, e.Message = "error", "read failed: "+err.Error()
		}
		m.log(e)
		return err
	}
	var write func(string, io.WriterTo) error
	if cfg.Writable {
		write = func(filename string, wt io.WriterTo) error {
			remote := remoteOf(wt)
			p, err := resolvePath(cfg.Root, filename)
			if err != nil {
				m.log(LogEntry{Kind: KindTFTP, Remote: remote, Action: "error", File: filename, Message: err.Error()})
				return err
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			f, err := os.Create(p)
			if err != nil {
				m.log(LogEntry{Kind: KindTFTP, Remote: remote, Action: "error", File: filename, Message: err.Error()})
				return err
			}
			m.touch(KindTFTP)
			n, err := wt.WriteTo(f)
			cerr := f.Close()
			if err == nil {
				err = cerr
			}
			e := LogEntry{Kind: KindTFTP, Remote: remote, Action: "write", File: filename, Bytes: n}
			if err != nil {
				e.Action, e.Message = "error", "write failed: "+err.Error()
				_ = os.Remove(p)
			}
			m.log(e)
			return err
		}
	}
	s := tftp.NewServer(read, write)
	s.SetTimeout(5 * time.Second)
	s.SetBlockSizeNegotiation(true)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	t := &tftpServer{s: s, conn: conn, done: make(chan struct{})}
	go func() {
		defer close(t.done)
		_ = s.Serve(conn)
	}()
	return t, nil
}
