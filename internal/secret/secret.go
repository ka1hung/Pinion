// Package secret stores passwords and key passphrases in the OS credential
// store (Windows Credential Manager, macOS Keychain, Linux Secret Service).
// When no OS store is reachable (e.g. headless Linux), it falls back to a
// file encrypted with a master password (Argon2id + AES-256-GCM).
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/argon2"
)

const service = "Pinion"

var (
	ErrNotFound = errors.New("secret not found")
	// ErrLocked means the fallback vault needs the master password.
	ErrLocked = errors.New("secret vault is locked")
)

// Kinds of secrets attached to a session.
const (
	KindPassword   = "password"
	KindPassphrase = "passphrase"
)

// Key builds a keychain account name.
func Key(sessionID, kind string) string { return "session:" + sessionID + ":" + kind }

// Store is the secret store.
type Store struct {
	path string // fallback vault file

	mu       sync.Mutex
	probed   bool
	useOS    bool
	unlocked bool
	master   []byte
	vault    map[string]string
	salt     []byte
}

// New creates a store; vaultPath is used only if the OS store is unavailable.
func New(vaultPath string) *Store { return &Store{path: vaultPath} }

func (s *Store) probe() {
	if s.probed {
		return
	}
	s.probed = true
	if os.Getenv("PINION_FORCE_VAULT") == "1" {
		return
	}
	const probeKey = "probe"
	if err := keyring.Set(service, probeKey, "ok"); err == nil {
		_ = keyring.Delete(service, probeKey)
		s.useOS = true
	}
}

// UsesOSStore reports whether the OS credential store is used.
func (s *Store) UsesOSStore() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probe()
	return s.useOS
}

// NeedsUnlock reports whether the fallback vault must be unlocked first.
func (s *Store) NeedsUnlock() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probe()
	return !s.useOS && !s.unlocked
}

// VaultExists reports whether a fallback vault file already exists.
func (s *Store) VaultExists() bool {
	_, err := os.Stat(s.path)
	return err == nil
}

type vaultFile struct {
	Salt  []byte `json:"salt"`
	Nonce []byte `json:"nonce"`
	Data  []byte `json:"data"`
}

func deriveKey(pw, salt []byte) []byte {
	return argon2.IDKey(pw, salt, 3, 64*1024, 4, 32)
}

// Unlock opens (or creates) the fallback vault with the master password.
func (s *Store) Unlock(master string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.salt = make([]byte, 16)
		_, _ = rand.Read(s.salt)
		s.master = []byte(master)
		s.vault = map[string]string{}
		s.unlocked = true
		return s.flushLocked()
	}
	if err != nil {
		return err
	}
	var vf vaultFile
	if err := json.Unmarshal(b, &vf); err != nil {
		return fmt.Errorf("vault corrupt: %w", err)
	}
	block, _ := aes.NewCipher(deriveKey([]byte(master), vf.Salt))
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, vf.Nonce, vf.Data, nil)
	if err != nil {
		return errors.New("wrong master password")
	}
	m := map[string]string{}
	if err := json.Unmarshal(plain, &m); err != nil {
		return err
	}
	s.salt, s.master, s.vault, s.unlocked = vf.Salt, []byte(master), m, true
	return nil
}

func (s *Store) flushLocked() error {
	plain, _ := json.Marshal(s.vault)
	block, _ := aes.NewCipher(deriveKey(s.master, s.salt))
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(nonce)
	vf := vaultFile{Salt: s.salt, Nonce: nonce, Data: gcm.Seal(nil, nonce, plain, nil)}
	b, _ := json.Marshal(vf)
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Get returns a secret or ErrNotFound.
func (s *Store) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probe()
	if s.useOS {
		v, err := keyring.Get(service, key)
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		return v, err
	}
	if !s.unlocked {
		return "", ErrLocked
	}
	v, ok := s.vault[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// Set stores a secret.
func (s *Store) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probe()
	if s.useOS {
		return keyring.Set(service, key, value)
	}
	if !s.unlocked {
		return ErrLocked
	}
	s.vault[key] = value
	return s.flushLocked()
}

// Delete removes a secret; missing secrets are not an error.
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probe()
	if s.useOS {
		err := keyring.Delete(service, key)
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return err
	}
	if !s.unlocked {
		return ErrLocked
	}
	delete(s.vault, key)
	return s.flushLocked()
}

// Has reports whether a secret exists (false on any error).
func (s *Store) Has(key string) bool {
	_, err := s.Get(key)
	return err == nil
}
