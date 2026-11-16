package transport

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"

	pty "github.com/aymanbagabas/go-pty"

	"github.com/ka1hung/pinion/internal/store"
)

// Local is a local shell on a PTY (ConPTY on Windows).
type Local struct {
	Config *store.LocalConfig
	Cols   int
	Rows   int

	mu  sync.Mutex
	p   pty.Pty
	cmd *pty.Cmd
}

// DefaultShell returns the user's shell.
func DefaultShell() string {
	if runtime.GOOS == "windows" {
		for _, s := range []string{"pwsh.exe", "powershell.exe"} {
			if p, err := lookPath(s); err == nil {
				return p
			}
		}
		if c := os.Getenv("COMSPEC"); c != "" {
			return c
		}
		return "cmd.exe"
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if runtime.GOOS == "darwin" {
		return "/bin/zsh"
	}
	return "/bin/bash"
}

func (t *Local) Open(ctx context.Context) error {
	p, err := pty.New()
	if err != nil {
		return err
	}
	cols, rows := t.Cols, t.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	_ = p.Resize(cols, rows)
	shell := DefaultShell()
	var args []string
	if t.Config != nil && t.Config.Shell != "" {
		shell, args = t.Config.Shell, t.Config.Args
	} else if runtime.GOOS != "windows" {
		args = []string{"-l"}
	}
	cmd := p.Command(shell, args...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=Pinion")
	if t.Config != nil && t.Config.Cwd != "" {
		cmd.Dir = t.Config.Cwd
	} else if h, err := os.UserHomeDir(); err == nil {
		cmd.Dir = h
	}
	if err := cmd.Start(); err != nil {
		p.Close()
		return fmt.Errorf("start %s: %w", shell, err)
	}
	t.mu.Lock()
	t.p, t.cmd = p, cmd
	t.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		// closing the pty unblocks Read with EOF
		_ = p.Close()
	}()
	return nil
}

func (t *Local) Read(b []byte) (int, error) {
	t.mu.Lock()
	p := t.p
	t.mu.Unlock()
	if p == nil {
		return 0, ErrClosed
	}
	return p.Read(b)
}

func (t *Local) Write(b []byte) (int, error) {
	t.mu.Lock()
	p := t.p
	t.mu.Unlock()
	if p == nil {
		return 0, ErrClosed
	}
	return p.Write(b)
}

func (t *Local) Resize(cols, rows int) error {
	t.mu.Lock()
	p := t.p
	t.mu.Unlock()
	if p == nil {
		return nil
	}
	return p.Resize(cols, rows)
}

func (t *Local) Close() error {
	t.mu.Lock()
	p, cmd := t.p, t.cmd
	t.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if p != nil {
		return p.Close()
	}
	return nil
}

func (t *Local) Describe() string { return "Local shell" }
