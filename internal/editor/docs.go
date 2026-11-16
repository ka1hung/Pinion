package editor

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
)

// Document is an open file as seen by the UI. Content is UTF-8 with LF.
type Document struct {
	ID       string `json:"id"`
	Source   string `json:"source"` // local | remote
	TabID    string `json:"tabId"`
	Path     string `json:"path"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	EOL      string `json:"eol"`   // LF | CRLF
	Mtime    int64  `json:"mtime"` // unix ms
	Size     int64  `json:"size"`
	ReadOnly bool   `json:"readOnly"`
	TooLarge bool   `json:"tooLarge"`
}

// SaveRequest comes from the UI.
type SaveRequest struct {
	DocID    string `json:"docId"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	EOL      string `json:"eol"`
	Force    bool   `json:"force"`
}

// SaveResult tells the UI what happened.
type SaveResult struct {
	OK            bool   `json:"ok"`
	Conflict      bool   `json:"conflict"`
	RemoteContent string `json:"remoteContent"`
	NeedSudo      bool   `json:"needSudo"`
	Mtime         int64  `json:"mtime"`
	Size          int64  `json:"size"`
	Message       string `json:"message"`
}

// Sources.
const (
	SourceLocal  = "local"
	SourceRemote = "remote"
)

// Resolver returns the filesystem for a document source.
type Resolver func(source, tabID string) (FS, error)

// IsPermission reports permission errors (pluggable for SFTP status codes).
type PermChecker func(error) bool

// Manager tracks open documents.
type Manager struct {
	resolve Resolver
	limit   func() int64 // bytes
	isPerm  PermChecker
	seq     atomic.Uint64
	mu      sync.Mutex
	docs    map[string]*Document // Content not kept
}

// NewManager creates a manager. limit returns the big-file limit in bytes.
func NewManager(r Resolver, limit func() int64, isPerm PermChecker) *Manager {
	if isPerm == nil {
		isPerm = func(err error) bool { return errors.Is(err, os.ErrPermission) }
	}
	return &Manager{resolve: r, limit: limit, isPerm: isPerm, docs: map[string]*Document{}}
}

func baseName(source, p string) string {
	if source == SourceRemote {
		return path.Base(p)
	}
	return filepath.Base(p)
}

// Open reads a file into a new document.
func (m *Manager) Open(source, tabID, p, encoding string, force bool) (*Document, error) {
	fs, err := m.resolve(source, tabID)
	if err != nil {
		return nil, err
	}
	st, err := fs.Stat(p)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s is a directory", p)
	}
	d := &Document{
		ID:     "d" + strconv.FormatUint(m.seq.Add(1), 10),
		Source: source, TabID: tabID, Path: p, Name: baseName(source, p),
		Mtime: st.ModTime().UnixMilli(), Size: st.Size(), Encoding: encoding, EOL: LF,
	}
	if lim := m.limit(); lim > 0 && st.Size() > lim {
		if !force {
			d.TooLarge = true
			return d, nil
		}
		d.ReadOnly = true
	}
	if err := m.load(fs, d, encoding); err != nil {
		return nil, err
	}
	m.mu.Lock()
	meta := *d
	meta.Content = ""
	m.docs[d.ID] = &meta
	m.mu.Unlock()
	return d, nil
}

func (m *Manager) load(fs FS, d *Document, encoding string) error {
	raw, err := fs.ReadFile(d.Path)
	if err != nil {
		return err
	}
	text, enc, eol, err := Decode(raw, encoding)
	if err != nil {
		return err
	}
	d.Content, d.Encoding, d.EOL, d.Size = text, enc, eol, int64(len(raw))
	return nil
}

// Get returns document metadata (without content).
func (m *Manager) Get(id string) (Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.docs[id]
	if d == nil {
		return Document{}, fmt.Errorf("document %s not open", id)
	}
	return *d, nil
}

// Reload re-reads the file (optionally with another encoding).
func (m *Manager) Reload(id, encoding string) (*Document, error) {
	meta, err := m.Get(id)
	if err != nil {
		return nil, err
	}
	fs, err := m.resolve(meta.Source, meta.TabID)
	if err != nil {
		return nil, err
	}
	st, err := fs.Stat(meta.Path)
	if err != nil {
		return nil, err
	}
	d := meta
	d.Mtime = st.ModTime().UnixMilli()
	if err := m.load(fs, &d, encoding); err != nil {
		return nil, err
	}
	m.mu.Lock()
	cp := d
	cp.Content = ""
	m.docs[id] = &cp
	m.mu.Unlock()
	return &d, nil
}

// Close forgets a document.
func (m *Manager) Close(id string) {
	m.mu.Lock()
	delete(m.docs, id)
	m.mu.Unlock()
}

// CloseTab forgets all documents of a tab.
func (m *Manager) CloseTab(tabID string) {
	m.mu.Lock()
	for id, d := range m.docs {
		if d.TabID == tabID && d.Source == SourceRemote {
			delete(m.docs, id)
		}
	}
	m.mu.Unlock()
}

// Changed reports whether the file differs from what was recorded.
func Changed(recMtime, recSize int64, st os.FileInfo) bool {
	return st.ModTime().UnixMilli() != recMtime || st.Size() != recSize
}

// CheckConflict stats the file; if it changed since open/last save it returns
// conflict=true and the current content decoded for a diff view.
func (m *Manager) CheckConflict(id string) (bool, string, error) {
	meta, err := m.Get(id)
	if err != nil {
		return false, "", err
	}
	fs, err := m.resolve(meta.Source, meta.TabID)
	if err != nil {
		return false, "", err
	}
	st, err := fs.Stat(meta.Path)
	if errors.Is(err, os.ErrNotExist) {
		return false, "", nil // deleted remotely: saving recreates it
	}
	if err != nil {
		return false, "", err
	}
	if !Changed(meta.Mtime, meta.Size, st) {
		return false, "", nil
	}
	raw, err := fs.ReadFile(meta.Path)
	if err != nil {
		return true, "", nil
	}
	text, _, _, _ := Decode(raw, meta.Encoding)
	return true, text, nil
}

// Save writes the document.
func (m *Manager) Save(req SaveRequest) (*SaveResult, error) {
	meta, err := m.Get(req.DocID)
	if err != nil {
		return nil, err
	}
	if meta.ReadOnly {
		return &SaveResult{Message: "document is read-only"}, nil
	}
	fs, err := m.resolve(meta.Source, meta.TabID)
	if err != nil {
		return nil, err
	}
	if !req.Force {
		conflict, remote, err := m.CheckConflict(req.DocID)
		if err != nil {
			return nil, err
		}
		if conflict {
			return &SaveResult{Conflict: true, RemoteContent: remote, Message: "file was modified since it was opened"}, nil
		}
	}
	enc, eol := req.Encoding, req.EOL
	if enc == "" {
		enc = meta.Encoding
	}
	if eol == "" {
		eol = meta.EOL
	}
	data := Encode(req.Content, enc, eol)
	if err := fs.WriteFile(meta.Path, data); err != nil {
		if m.isPerm(err) {
			return &SaveResult{NeedSudo: meta.Source == SourceRemote, Message: "permission denied"}, nil
		}
		return nil, err
	}
	return m.AfterWrite(req.DocID, enc, eol)
}

// AfterWrite re-stats the file and records the new mtime/size.
func (m *Manager) AfterWrite(id, enc, eol string) (*SaveResult, error) {
	meta, err := m.Get(id)
	if err != nil {
		return nil, err
	}
	fs, err := m.resolve(meta.Source, meta.TabID)
	if err != nil {
		return nil, err
	}
	st, err := fs.Stat(meta.Path)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if d := m.docs[id]; d != nil {
		d.Mtime, d.Size = st.ModTime().UnixMilli(), st.Size()
		d.Encoding, d.EOL = enc, eol
	}
	m.mu.Unlock()
	return &SaveResult{OK: true, Mtime: st.ModTime().UnixMilli(), Size: st.Size()}, nil
}
