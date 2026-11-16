package app

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ka1hung/pinion/internal/secret"
	"github.com/ka1hung/pinion/internal/store"
)

// SessionService manages saved sessions and folders.
type SessionService struct{ c *Core }

// NewSessionService creates the service.
func NewSessionService(c *Core) *SessionService { return &SessionService{c: c} }

// SessionTree is the full sidebar state.
type SessionTree struct {
	Folders  []string         `json:"folders"`
	Sessions []*store.Session `json:"sessions"`
}

// Tree returns all folders and sessions.
func (s *SessionService) Tree() SessionTree {
	return SessionTree{Folders: s.c.Store.Folders(), Sessions: s.c.Store.List()}
}

// Get returns one session.
func (s *SessionService) Get(id string) (*store.Session, error) { return s.c.Store.Get(id) }

// Save creates or updates a session.
func (s *SessionService) Save(sess store.Session) (*store.Session, error) {
	out, err := s.c.Store.Save(&sess)
	if err == nil {
		s.c.Emit("sessions:changed", nil)
	}
	return out, err
}

// Delete removes a session and its stored secrets.
func (s *SessionService) Delete(id string) error {
	if err := s.c.Store.Delete(id); err != nil {
		return err
	}
	_ = s.c.Secrets.Delete(secret.Key(id, secret.KindPassword))
	_ = s.c.Secrets.Delete(secret.Key(id, secret.KindPassphrase))
	s.c.Emit("sessions:changed", nil)
	return nil
}

// Duplicate copies a session (secrets are not copied).
func (s *SessionService) Duplicate(id string) (*store.Session, error) {
	out, err := s.c.Store.Duplicate(id)
	if err == nil {
		s.c.Emit("sessions:changed", nil)
	}
	return out, err
}

// Move puts a session in another folder.
func (s *SessionService) Move(id, folder string) error {
	err := s.c.Store.MoveSession(id, folder)
	if err == nil {
		s.c.Emit("sessions:changed", nil)
	}
	return err
}

// AddFolder creates an empty folder.
func (s *SessionService) AddFolder(folder string) error {
	err := s.c.Store.AddFolder(folder)
	if err == nil {
		s.c.Emit("sessions:changed", nil)
	}
	return err
}

// RenameFolder renames/moves a folder.
func (s *SessionService) RenameFolder(from, to string) error {
	err := s.c.Store.RenameFolder(from, to)
	if err == nil {
		s.c.Emit("sessions:changed", nil)
	}
	return err
}

// DeleteFolder deletes a folder; sessions move up unless deleteSessions.
func (s *SessionService) DeleteFolder(folder string, deleteSessions bool) error {
	if deleteSessions {
		for _, x := range s.c.Store.List() {
			if x.Folder == folder || len(x.Folder) > len(folder) && x.Folder[:len(folder)+1] == folder+"/" {
				_ = s.c.Secrets.Delete(secret.Key(x.ID, secret.KindPassword))
				_ = s.c.Secrets.Delete(secret.Key(x.ID, secret.KindPassphrase))
			}
		}
	}
	err := s.c.Store.DeleteFolder(folder, deleteSessions)
	if err == nil {
		s.c.Emit("sessions:changed", nil)
	}
	return err
}

// SetSecret stores a password ("password") or key passphrase ("passphrase").
func (s *SessionService) SetSecret(id, kind, value string) error {
	if kind != secret.KindPassword && kind != secret.KindPassphrase {
		return errors.New("bad secret kind")
	}
	if value == "" {
		return s.c.Secrets.Delete(secret.Key(id, kind))
	}
	return s.c.Secrets.Set(secret.Key(id, kind), value)
}

// HasSecret reports whether a secret is stored.
func (s *SessionService) HasSecret(id, kind string) bool {
	return s.c.Secrets.Has(secret.Key(id, kind))
}

// ClearSecret removes a stored secret.
func (s *SessionService) ClearSecret(id, kind string) error {
	return s.c.Secrets.Delete(secret.Key(id, kind))
}

// Export writes sessions of a folder ("" = all) to a JSON file chosen by
// the user. Passwords are never exported. Returns the path ("" if cancelled).
func (s *SessionService) Export(folder string) (string, error) {
	b, err := s.c.Store.Export(folder)
	if err != nil {
		return "", err
	}
	name := "pinion-sessions.json"
	if folder != "" {
		name = "pinion-" + filepath.Base(folder) + ".json"
	}
	p, err := s.c.PickSave("Export sessions", name, []FileFilter{{"Pinion sessions", "*.json"}})
	if err != nil || p == "" {
		return "", err
	}
	return p, os.WriteFile(p, b, 0o600)
}

// ImportPinion imports a file produced by Export under an optional folder.
// Returns the number of sessions imported (0 if cancelled).
func (s *SessionService) ImportPinion(targetFolder string) (int, error) {
	p, err := s.c.PickFile("Import Pinion sessions", []FileFilter{{"Pinion sessions", "*.json"}})
	if err != nil || p == "" {
		return 0, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return 0, err
	}
	d, err := store.ParseExport(b)
	if err != nil {
		return 0, err
	}
	n, err := s.c.Store.ImportData(d, targetFolder)
	if err == nil {
		s.c.Emit("sessions:changed", nil)
	}
	return n, err
}
