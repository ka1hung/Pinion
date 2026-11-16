package app

import (
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/ka1hung/pinion/internal/charset"
	"github.com/ka1hung/pinion/internal/prompt"
	"github.com/ka1hung/pinion/internal/sshx"
	"github.com/ka1hung/pinion/internal/store"
)

// Version is set at build time via -ldflags "-X .../internal/app.Version=v1.2.3".
var Version = "dev"

// SystemService exposes settings, prompts and misc OS helpers.
type SystemService struct{ c *Core }

// NewSystemService creates the service.
func NewSystemService(c *Core) *SystemService { return &SystemService{c: c} }

// AppInfo describes the running app.
type AppInfo struct {
	Version     string `json:"version"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	ConfigDir   string `json:"configDir"`
	OSKeychain  bool   `json:"osKeychain"`
	VaultLocked bool   `json:"vaultLocked"`
	VaultExists bool   `json:"vaultExists"`
	Agent       bool   `json:"agent"`
}

// Info returns app/platform info.
func (s *SystemService) Info() AppInfo {
	return AppInfo{
		Version: Version, OS: runtime.GOOS, Arch: runtime.GOARCH, ConfigDir: s.c.Store.Dir(),
		OSKeychain: s.c.Secrets.UsesOSStore(), VaultLocked: s.c.Secrets.NeedsUnlock(),
		VaultExists: s.c.Secrets.VaultExists(), Agent: sshx.AgentAvailable(),
	}
}

// Settings returns preferences.
func (s *SystemService) Settings() store.Settings { return s.c.Store.Settings() }

// SaveSettings stores preferences.
func (s *SystemService) SaveSettings(st store.Settings) error {
	err := s.c.Store.SaveSettings(st)
	if err == nil {
		s.c.Emit("settings:changed", st)
	}
	return err
}

// AnswerPrompt delivers the user's answer to a backend prompt.
func (s *SystemService) AnswerPrompt(r prompt.Response) { s.c.Prompt.Answer(r) }

// UnlockVault unlocks (or creates) the fallback encrypted vault.
func (s *SystemService) UnlockVault(master string) error { return s.c.Secrets.Unlock(master) }

// KnownHosts lists known_hosts entries.
func (s *SystemService) KnownHosts() ([]sshx.Entry, error) { return s.c.Dialer.HostKeys().List() }

// RemoveKnownHost deletes a known_hosts line.
func (s *SystemService) RemoveKnownHost(line int) error {
	return s.c.Dialer.HostKeys().RemoveLine(line)
}

// Charsets lists supported encodings.
func (s *SystemService) Charsets() []string { return charset.Supported }

// PickFile shows an open-file dialog.
func (s *SystemService) PickFile(title string, filters []FileFilter) (string, error) {
	return s.c.PickFile(title, filters)
}

// PickFolder shows a folder dialog.
func (s *SystemService) PickFolder(title string) (string, error) { return s.c.PickFolder(title) }

// OpenURL opens a link in the default browser.
func (s *SystemService) OpenURL(url string) error { return s.c.App.Browser.OpenURL(url) }

// OpenPath opens a file or folder with the OS default handler.
func (s *SystemService) OpenPath(path string) error {
	if _, err := os.Stat(path); err != nil {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	return s.c.App.Browser.OpenFile(path)
}

// HomeDir returns the user's home directory.
func (s *SystemService) HomeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// SetWindowTheme matches the native title bar to the app theme.
// Colours are CSS hex strings like "#f8f8f8".
func (s *SystemService) SetWindowTheme(dark bool, titleBar, titleText string) {
	applyWindowTheme(s.c.Win, dark, parseHex(titleBar), parseHex(titleText))
}

func parseHex(c string) uint32 {
	c = strings.TrimPrefix(strings.TrimSpace(c), "#")
	if len(c) == 3 {
		c = string([]byte{c[0], c[0], c[1], c[1], c[2], c[2]})
	}
	v, _ := strconv.ParseUint(c, 16, 32)
	return uint32(v)
}
