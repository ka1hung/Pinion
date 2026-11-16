// Package termlog writes per-tab session logs. File names include the site
// folder, session name and start time.
package termlog

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Options controls formatting.
type Options struct {
	Dir        string
	Folder     string
	Name       string
	StripANSI  bool
	Timestamps bool
}

// Logger is safe for concurrent use.
type Logger struct {
	mu      sync.Mutex
	f       *os.File
	w       *bufio.Writer
	opts    Options
	atStart bool
	path    string
	stopped chan struct{}
}

var unsafeChars = regexp.MustCompile(`[<>:"|?*\x00-\x1f]`)

// Sanitize makes a string safe as a path element on every OS.
func Sanitize(s string) string {
	s = unsafeChars.ReplaceAllString(s, "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, `\`, "_")
	s = strings.Trim(s, " .")
	if s == "" {
		s = "session"
	}
	return s
}

// Path builds the log file path for a new log.
func Path(o Options, t time.Time) string {
	dir := o.Dir
	for _, p := range strings.Split(o.Folder, "/") {
		if p != "" {
			dir = filepath.Join(dir, Sanitize(p))
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s_%s.log", Sanitize(o.Name), t.Format("20060102-150405")))
}

// Open creates the log file.
func Open(o Options) (*Logger, error) {
	p := Path(o, time.Now())
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	l := &Logger{f: f, w: bufio.NewWriterSize(f, 64*1024), opts: o, atStart: true, path: p, stopped: make(chan struct{})}
	go l.flusher()
	return l, nil
}

// FilePath returns the log path.
func (l *Logger) FilePath() string { return l.path }

func (l *Logger) flusher() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-l.stopped:
			return
		case <-t.C:
			l.mu.Lock()
			if l.w != nil {
				_ = l.w.Flush()
			}
			l.mu.Unlock()
		}
	}
}

// ANSI CSI / OSC / single-char escape sequences.
var ansiRe = regexp.MustCompile(`\x1b(\[[0-?]*[ -/]*[@-~]|\][^\x07\x1b]*(\x07|\x1b\\)|[@-Z\\-_]|[()][0-9A-Za-z])`)

// StripANSI removes terminal escape sequences and stray CRs before LF.
func StripANSI(b []byte) []byte {
	b = ansiRe.ReplaceAll(b, nil)
	return []byte(strings.ReplaceAll(string(b), "\r\n", "\n"))
}

// Write appends terminal output.
func (l *Logger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil {
		return len(p), nil
	}
	data := p
	if l.opts.StripANSI {
		data = StripANSI(p)
	}
	if !l.opts.Timestamps {
		_, err := l.w.Write(data)
		return len(p), err
	}
	for len(data) > 0 {
		if l.atStart {
			_, _ = l.w.WriteString(time.Now().Format("[2006-01-02 15:04:05.000] "))
			l.atStart = false
		}
		i := strings.IndexByte(string(data), '\n')
		if i < 0 {
			_, _ = l.w.Write(data)
			break
		}
		_, _ = l.w.Write(data[:i+1])
		data = data[i+1:]
		l.atStart = true
	}
	return len(p), nil
}

// Note writes a marker line (connect/disconnect).
func (l *Logger) Note(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil {
		return
	}
	prefix := ""
	if !l.atStart {
		prefix = "\n"
	}
	fmt.Fprintf(l.w, "%s=== %s %s ===\n", prefix, time.Now().Format("2006-01-02 15:04:05"), msg)
	l.atStart = true
}

// Close flushes and closes.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.w == nil {
		return nil
	}
	close(l.stopped)
	_ = l.w.Flush()
	err := l.f.Close()
	l.w, l.f = nil, nil
	return err
}
