package editor

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// External editor states.
const (
	ExtWatching = "watching"
	ExtSynced   = "synced"
	ExtError    = "error"
	ExtConflict = "conflict"
	ExtStopped  = "stopped"
)

// ExternalState is emitted as "editor:external".
type ExternalState struct {
	ID        string `json:"id"`
	TabID     string `json:"tabId"`
	Path      string `json:"path"`
	LocalPath string `json:"localPath"`
	State     string `json:"state"`
	Message   string `json:"message"`
	Time      int64  `json:"time"`
}

// External syncs temp copies edited by an external program back to the remote.
type External struct {
	resolve  Resolver
	emit     func(ExternalState)
	baseDir  string
	debounce time.Duration

	seq      atomic.Uint64
	mu       sync.Mutex
	sessions map[string]*extSession
}

type extSession struct {
	mu       sync.Mutex
	st       ExternalState
	dir      string
	watcher  *fsnotify.Watcher
	timer    *time.Timer
	mtime    int64
	size     int64
	lastHash [32]byte
	stopped  bool
}

// NewExternal creates the manager; baseDir is e.g. os.TempDir()/pinion-edit.
func NewExternal(r Resolver, baseDir string, emit func(ExternalState)) *External {
	if emit == nil {
		emit = func(ExternalState) {}
	}
	return &External{resolve: r, emit: emit, baseDir: baseDir, debounce: 400 * time.Millisecond, sessions: map[string]*extSession{}}
}

var unsafePath = regexp.MustCompile(`[^A-Za-z0-9._@-]+`)

// SanitizeDir makes a host/tab label safe as a directory name.
func SanitizeDir(s string) string {
	s = unsafePath.ReplaceAllString(s, "_")
	if s == "" || s == "." || s == ".." {
		s = "remote"
	}
	return s
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Open downloads remotePath of tabID into a temp dir, starts watching it and
// calls launch with the local path. Returns the session ID.
func (x *External) Open(tabID, label, remotePath string, launch func(local string) error) (string, error) {
	fs, err := x.resolve(SourceRemote, tabID)
	if err != nil {
		return "", err
	}
	st, err := fs.Stat(remotePath)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("%s is a directory", remotePath)
	}
	raw, err := fs.ReadFile(remotePath)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(x.baseDir, SanitizeDir(label), randHex(6))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	name := path.Base(remotePath)
	if name == "" || name == "/" || name == "." {
		name = "file"
	}
	local := filepath.Join(dir, name)
	if err := os.WriteFile(local, raw, 0o600); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	if err := w.Add(dir); err != nil {
		w.Close()
		os.RemoveAll(dir)
		return "", err
	}
	s := &extSession{
		st: ExternalState{
			ID: "x" + strconv.FormatUint(x.seq.Add(1), 10), TabID: tabID, Path: remotePath, LocalPath: local,
			State: ExtWatching, Time: time.Now().UnixMilli(),
		},
		dir: dir, watcher: w, mtime: st.ModTime().UnixMilli(), size: st.Size(), lastHash: sha256.Sum256(raw),
	}
	x.mu.Lock()
	x.sessions[s.st.ID] = s
	x.mu.Unlock()
	go x.watch(s, name)
	if launch != nil {
		if err := launch(local); err != nil {
			x.Stop(s.st.ID)
			return "", fmt.Errorf("launch editor: %w", err)
		}
	}
	x.emit(s.snapshot())
	return s.st.ID, nil
}

func (s *extSession) snapshot() ExternalState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

func (x *External) setState(s *extSession, state, msg string) {
	s.mu.Lock()
	s.st.State, s.st.Message, s.st.Time = state, msg, time.Now().UnixMilli()
	st := s.st
	s.mu.Unlock()
	x.emit(st)
}

func (x *External) watch(s *extSession, name string) {
	for {
		select {
		case ev, ok := <-s.watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(ev.Name) != name {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			s.mu.Lock()
			if s.stopped {
				s.mu.Unlock()
				return
			}
			if s.timer != nil {
				s.timer.Stop()
			}
			s.timer = time.AfterFunc(x.debounce, func() { x.sync(s, false) })
			s.mu.Unlock()
		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			x.setState(s, ExtError, err.Error())
		}
	}
}

// sync uploads the local copy if it changed. With force, the remote-changed
// check is skipped.
func (x *External) sync(s *extSession, force bool) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	local, remote, tabID := s.st.LocalPath, s.st.Path, s.st.TabID
	recM, recS, last := s.mtime, s.size, s.lastHash
	s.mu.Unlock()

	data, err := os.ReadFile(local)
	if err != nil {
		x.setState(s, ExtError, err.Error())
		return
	}
	h := sha256.Sum256(data)
	if h == last && !force {
		return
	}
	fs, err := x.resolve(SourceRemote, tabID)
	if err != nil {
		x.setState(s, ExtError, err.Error())
		return
	}
	if !force {
		if st, err := fs.Stat(remote); err == nil && Changed(recM, recS, st) {
			x.setState(s, ExtConflict, "remote file changed since it was downloaded; not uploaded (use Force upload to overwrite)")
			return
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			x.setState(s, ExtError, err.Error())
			return
		}
	}
	if err := fs.WriteFile(remote, data); err != nil {
		x.setState(s, ExtError, "upload failed: "+err.Error())
		return
	}
	st, err := fs.Stat(remote)
	s.mu.Lock()
	s.lastHash = h
	if err == nil {
		s.mtime, s.size = st.ModTime().UnixMilli(), st.Size()
	}
	s.mu.Unlock()
	x.setState(s, ExtSynced, fmt.Sprintf("uploaded %d bytes", len(data)))
}

// ForceUpload uploads the local copy ignoring remote changes.
func (x *External) ForceUpload(id string) error {
	x.mu.Lock()
	s := x.sessions[id]
	x.mu.Unlock()
	if s == nil {
		return fmt.Errorf("external edit %s not found", id)
	}
	x.sync(s, true)
	return nil
}

// List returns all sessions.
func (x *External) List() []ExternalState {
	x.mu.Lock()
	defer x.mu.Unlock()
	out := make([]ExternalState, 0, len(x.sessions))
	for _, s := range x.sessions {
		out = append(out, s.snapshot())
	}
	return out
}

// Stop ends a session and deletes its temp copy.
func (x *External) Stop(id string) {
	x.mu.Lock()
	s := x.sessions[id]
	delete(x.sessions, id)
	x.mu.Unlock()
	if s == nil {
		return
	}
	s.mu.Lock()
	s.stopped = true
	if s.timer != nil {
		s.timer.Stop()
	}
	s.mu.Unlock()
	_ = s.watcher.Close()
	_ = os.RemoveAll(s.dir)
	x.setState(s, ExtStopped, "")
}

// StopTab stops all sessions of a tab.
func (x *External) StopTab(tabID string) {
	x.mu.Lock()
	var ids []string
	for id, s := range x.sessions {
		if s.st.TabID == tabID {
			ids = append(ids, id)
		}
	}
	x.mu.Unlock()
	for _, id := range ids {
		x.Stop(id)
	}
}

// StopAll stops everything and removes the base temp dir.
func (x *External) StopAll() {
	x.mu.Lock()
	var ids []string
	for id := range x.sessions {
		ids = append(ids, id)
	}
	x.mu.Unlock()
	for _, id := range ids {
		x.Stop(id)
	}
	_ = os.RemoveAll(x.baseDir)
}
