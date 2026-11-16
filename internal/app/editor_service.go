package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ka1hung/pinion/internal/editor"
	"github.com/ka1hung/pinion/internal/secret"
	"github.com/ka1hung/pinion/internal/sftpx"
)

// EditorService backs the built-in Monaco editor and the external editor sync.
type EditorService struct {
	c    *Core
	m    *sftpx.Manager
	docs *editor.Manager
	ext  *editor.External
}

// NewEditorService creates the service.
func NewEditorService(c *Core) *EditorService {
	s := &EditorService{c: c, m: sftpManager(c)}
	resolve := func(source, tabID string) (editor.FS, error) {
		if source == editor.SourceLocal {
			return editor.LocalFS{}, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cl, _, err := s.m.Client(ctx, tabID)
		if err != nil {
			return nil, err
		}
		return editor.RemoteFS{C: cl}, nil
	}
	limit := func() int64 {
		mb := c.Store.Settings().BigFileLimitMB
		if mb <= 0 {
			return 0
		}
		return int64(mb) << 20
	}
	s.docs = editor.NewManager(resolve, limit, sftpx.IsPermission)
	s.ext = editor.NewExternal(resolve, filepath.Join(os.TempDir(), "pinion-edit"), func(st editor.ExternalState) {
		c.Emit("editor:external", st)
	})
	c.Tabs.OnClose(func(tabID string) {
		s.docs.CloseTab(tabID)
		s.ext.StopTab(tabID)
	})
	c.OnShutdown(s.ext.StopAll)
	return s
}

// OpenRemote opens a remote file of an SSH tab. encoding "" = auto-detect.
// force opens files above the big-file limit read-only.
func (s *EditorService) OpenRemote(tabID, path, encoding string, force bool) (*editor.Document, error) {
	return s.docs.Open(editor.SourceRemote, tabID, path, encoding, force)
}

// OpenLocal opens a local file.
func (s *EditorService) OpenLocal(path, encoding string, force bool) (*editor.Document, error) {
	return s.docs.Open(editor.SourceLocal, "", path, encoding, force)
}

// PickLocal shows an open dialog and returns the chosen path ("" if cancelled).
func (s *EditorService) PickLocal() (string, error) { return s.c.PickFile("Open file", nil) }

// Reload re-reads a document, optionally with another encoding.
func (s *EditorService) Reload(docID, encoding string) (*editor.Document, error) {
	return s.docs.Reload(docID, encoding)
}

// Close forgets a document.
func (s *EditorService) Close(docID string) { s.docs.Close(docID) }

// Save writes a document (see SaveResult for conflict / sudo outcomes).
func (s *EditorService) Save(req editor.SaveRequest) (*editor.SaveResult, error) {
	return s.docs.Save(req)
}

func randName() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// SudoCommand builds the remote command that moves tmp over target as root.
func SudoCommand(tmp, target string) string {
	script := `cat "$1" > "$2"; r=$?; rm -f "$1"; exit $r`
	return "sudo -S -p '' sh -c " + editor.ShellQuote(script) + " sh " + editor.ShellQuote(tmp) + " " + editor.ShellQuote(target)
}

// SaveSudo saves a remote document through sudo (upload to /tmp, then
// `sudo sh -c 'cat tmp > target'`, which keeps the target's owner and mode).
// An empty password uses the session's saved password if any.
func (s *EditorService) SaveSudo(docID, content, encoding, eol, password string) (*editor.SaveResult, error) {
	meta, err := s.docs.Get(docID)
	if err != nil {
		return nil, err
	}
	if meta.Source != editor.SourceRemote {
		return nil, errors.New("sudo save is only for remote files")
	}
	if encoding == "" {
		encoding = meta.Encoding
	}
	if eol == "" {
		eol = meta.EOL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cl, conn, err := s.m.Client(ctx, meta.TabID)
	if err != nil {
		return nil, err
	}
	if password == "" {
		if t, err := s.c.Tabs.Get(meta.TabID); err == nil && t.Session().ID != "" {
			if v, err := s.c.Secrets.Get(secret.Key(t.Session().ID, secret.KindPassword)); err == nil {
				password = v
			}
		}
	}
	data := editor.Encode(content, encoding, eol)
	tmp := "/tmp/.pinion-" + randName()
	f, err := cl.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return nil, fmt.Errorf("create temp file: %w", err)
	}
	_ = cl.Chmod(tmp, 0o600)
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = cl.Remove(tmp)
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = cl.Remove(tmp)
		return nil, err
	}
	defer func() { _ = cl.Remove(tmp) }() // usually already removed by the script

	sess, err := conn.Client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	var stderr, stdout bytes.Buffer
	sess.Stderr, sess.Stdout = &stderr, &stdout
	sess.Stdin = strings.NewReader(password + "\n")
	runErr := sess.Run(SudoCommand(tmp, meta.Path))
	if runErr != nil {
		msg := strings.TrimSpace(stderr.String() + "\n" + stdout.String())
		low := strings.ToLower(msg)
		switch {
		case strings.Contains(low, "incorrect password") || strings.Contains(low, "try again") || strings.Contains(low, "authentication failure"):
			return &editor.SaveResult{Message: "sudo: incorrect password"}, nil
		case strings.Contains(low, "not in the sudoers") || strings.Contains(low, "not allowed"):
			return &editor.SaveResult{Message: "sudo: user is not allowed to run sudo on this host"}, nil
		case strings.Contains(low, "command not found") && strings.Contains(low, "sudo"):
			return &editor.SaveResult{Message: "sudo is not installed on this host"}, nil
		}
		if msg == "" {
			msg = runErr.Error()
		}
		return &editor.SaveResult{Message: "sudo save failed: " + msg}, nil
	}
	return s.docs.AfterWrite(docID, encoding, eol)
}

func (s *EditorService) launchExternal(local string) error {
	st := s.c.Store.Settings()
	var argv []string
	if strings.TrimSpace(st.ExternalEditorCmd) != "" {
		a, err := editor.BuildCommand(st.ExternalEditorCmd, local)
		if err != nil {
			return err
		}
		argv = a
	} else if code := editor.DetectVSCode(); code != "" {
		argv = []string{code, local}
	}
	if argv == nil {
		if s.c.App == nil {
			return errors.New("no external editor configured")
		}
		return s.c.App.Browser.OpenFile(local)
	}
	cmd := editor.Command(argv)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// OpenExternal downloads a remote file to a temp folder, opens it in the
// external editor and uploads it back on every save. Returns the watch ID.
func (s *EditorService) OpenExternal(tabID, path string) (string, error) {
	label := tabID
	if t, err := s.c.Tabs.Get(tabID); err == nil {
		if ss := t.Session(); ss.SSH != nil && ss.SSH.Host != "" {
			label = ss.SSH.Host
		}
	}
	return s.ext.Open(tabID, label, path, s.launchExternal)
}

// OpenLocalExternal opens a local file in the external editor (no sync needed).
func (s *EditorService) OpenLocalExternal(path string) error { return s.launchExternal(path) }

// DetectExternalEditor returns the command that would be used.
func (s *EditorService) DetectExternalEditor() string {
	if c := strings.TrimSpace(s.c.Store.Settings().ExternalEditorCmd); c != "" {
		return c
	}
	if code := editor.DetectVSCode(); code != "" {
		return code
	}
	return "system default"
}

// ListExternal returns active external edits.
func (s *EditorService) ListExternal() []editor.ExternalState { return s.ext.List() }

// StopExternal stops syncing and deletes the temp copy.
func (s *EditorService) StopExternal(id string) { s.ext.Stop(id) }

// ForceUploadExternal uploads the local copy even if the remote changed.
func (s *EditorService) ForceUploadExternal(id string) error { return s.ext.ForceUpload(id) }
