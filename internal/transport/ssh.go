package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ka1hung/pinion/internal/sshx"
	"github.com/ka1hung/pinion/internal/store"
)

// SSH is an interactive shell over a pooled SSH connection.
type SSH struct {
	Pool    *sshx.Pool
	Session *store.Session
	Key     string // pool key (tab ID)
	TabID   string
	Cols    int
	Rows    int
	// Init is sent to the shell once it is ready (before StartupCommand).
	Init string

	lastRead atomic.Int64

	mu     sync.Mutex
	conn   *sshx.Conn
	sess   *ssh.Session
	stdin  io.WriteCloser
	stdout io.Reader
	closed bool
}

func (t *SSH) Open(ctx context.Context) error {
	conn, err := t.Pool.Acquire(ctx, t.Key, t.Session, t.TabID)
	if err != nil {
		return err
	}
	s, err := conn.Client.NewSession()
	if err != nil {
		t.Pool.Release(t.Key, conn)
		return fmt.Errorf("open session: %w", err)
	}
	term := t.Session.SSH.TermType
	if term == "" {
		term = "xterm-256color"
	}
	cols, rows := t.Cols, t.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 115200, ssh.TTY_OP_OSPEED: 115200}
	if err := s.RequestPty(term, rows, cols, modes); err != nil {
		s.Close()
		t.Pool.Release(t.Key, conn)
		return fmt.Errorf("request pty: %w", err)
	}
	_ = s.Setenv("LANG", "en_US.UTF-8")
	stdin, _ := s.StdinPipe()
	stdout, _ := s.StdoutPipe()
	s.Stderr = nil
	if err := s.Shell(); err != nil {
		s.Close()
		t.Pool.Release(t.Key, conn)
		return fmt.Errorf("start shell: %w", err)
	}
	t.mu.Lock()
	t.conn, t.sess, t.stdin, t.stdout = conn, s, stdin, stdout
	t.mu.Unlock()
	cmds := t.Init
	if cmd := t.Session.SSH.StartupCommand; cmd != "" {
		cmds += cmd + "\n"
	}
	if cmds != "" {
		go t.sendWhenIdle(stdin, cmds)
	}
	return nil
}

// sendWhenIdle waits until login output (banner, motd, prompt) has settled
// before typing commands, so the shell echoes them once at its prompt
// instead of the tty echoing them early as type-ahead.
func (t *SSH) sendWhenIdle(w io.Writer, cmds string) {
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		last := t.lastRead.Load()
		if last != 0 && time.Since(time.Unix(0, last)) > 300*time.Millisecond {
			break
		}
	}
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if !closed {
		_, _ = w.Write([]byte(cmds))
	}
}

func (t *SSH) Read(p []byte) (int, error) {
	t.mu.Lock()
	r := t.stdout
	t.mu.Unlock()
	if r == nil {
		return 0, ErrClosed
	}
	n, err := r.Read(p)
	if n > 0 {
		t.lastRead.Store(time.Now().UnixNano())
	}
	if err != nil && n == 0 {
		t.mu.Lock()
		c := t.conn
		t.mu.Unlock()
		if c != nil {
			select {
			case <-c.Done():
				if c.Err() != nil {
					return 0, fmt.Errorf("connection lost: %w", c.Err())
				}
			default:
			}
		}
		return 0, io.EOF
	}
	return n, nil
}

func (t *SSH) Write(p []byte) (int, error) {
	t.mu.Lock()
	w := t.stdin
	t.mu.Unlock()
	if w == nil {
		return 0, ErrClosed
	}
	return w.Write(p)
}

func (t *SSH) Resize(cols, rows int) error {
	t.mu.Lock()
	t.Cols, t.Rows = cols, rows
	s := t.sess
	t.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.WindowChange(rows, cols)
}

func (t *SSH) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	s, conn := t.sess, t.conn
	t.mu.Unlock()
	var err error
	if s != nil {
		err = s.Close()
		t.Pool.Release(t.Key, conn)
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return err
}

func (t *SSH) Describe() string {
	c := t.Session.SSH
	return fmt.Sprintf("SSH %s@%s:%d", c.User, c.Host, c.Port)
}
