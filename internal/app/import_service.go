package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ka1hung/pinion/internal/importer/bookmarks"
	"github.com/ka1hung/pinion/internal/store"
)

// ImportService imports sessions from session bookmark files (.ini / .mxtsessions).
type ImportService struct{ c *Core }

// NewImportService creates the service.
func NewImportService(c *Core) *ImportService { return &ImportService{c: c} }

const maxImportSize = 32 << 20

// DetectPaths returns existing session .ini files in their usual locations
// (Windows only; empty elsewhere).
func (s *ImportService) DetectPaths() []string {
	if runtime.GOOS != "windows" {
		return []string{}
	}
	var cands []string
	if d := os.Getenv("APPDATA"); d != "" {
		cands = append(cands, filepath.Join(d, "MobaXterm", "MobaXterm.ini"))
	}
	if h := os.Getenv("USERPROFILE"); h != "" {
		cands = append(cands,
			filepath.Join(h, "Documents", "MobaXterm", "MobaXterm.ini"),
			filepath.Join(h, "Desktop", "MobaXterm.ini"),
			filepath.Join(h, "Downloads", "MobaXterm.ini"),
		)
	}
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if d := os.Getenv(env); d != "" {
			cands = append(cands, filepath.Join(d, "Mobatek", "MobaXterm", "MobaXterm.ini"))
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, p := range cands {
		if seen[p] {
			continue
		}
		seen[p] = true
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			out = append(out, p)
		}
	}
	return out
}

// PickFile shows a dialog for *.ini / *.mxtsessions ("" if cancelled).
func (s *ImportService) PickFile() (string, error) {
	return s.c.PickFile("Import sessions", []FileFilter{
		{Name: "Session files (*.ini, *.mxtsessions)", Pattern: "*.ini;*.mxtsessions"},
		{Name: "All files", Pattern: "*"},
	})
}

func readImportFile(path string) (*bookmarks.Result, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > maxImportSize {
		return nil, errors.New("file too large for a session file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return bookmarks.Parse(b)
}

// Preview parses a file without saving anything.
func (s *ImportService) Preview(path string) (*bookmarks.Result, error) {
	return readImportFile(path)
}

// Commit imports all supported sessions under targetFolder ("" = root) and
// returns how many were imported.
func (s *ImportService) Commit(path, targetFolder string) (int, error) {
	r, err := readImportFile(path)
	if err != nil {
		return 0, err
	}
	prefix := store.CleanFolder(targetFolder)
	join := func(f string) string {
		f = store.CleanFolder(f)
		switch {
		case prefix == "":
			return f
		case f == "":
			return prefix
		}
		return prefix + "/" + f
	}
	if prefix != "" {
		if err := s.c.Store.AddFolder(prefix); err != nil {
			return 0, err
		}
	}
	for _, f := range r.Folders {
		if err := s.c.Store.AddFolder(join(f)); err != nil {
			return 0, err
		}
	}
	for _, sess := range r.Sessions {
		sess.Folder = join(sess.Folder)
	}
	if err := s.c.Store.SaveMany(r.Sessions); err != nil {
		return 0, err
	}
	s.c.Emit("sessions:changed", nil)
	return len(r.Sessions), nil
}
