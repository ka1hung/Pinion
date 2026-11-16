package sshx

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/ka1hung/pinion/internal/prompt"
)

// HostKeys manages an OpenSSH-compatible known_hosts file. Unknown keys are
// confirmed by the user; changed keys are always rejected (no "ignore").
type HostKeys struct {
	Path   string
	Prompt *prompt.Broker
	mu     sync.Mutex
}

// ErrHostKeyChanged is returned when a known host presents a different key.
var ErrHostKeyChanged = errors.New("REMOTE HOST KEY HAS CHANGED")

func (h *HostKeys) ensure() error {
	if err := os.MkdirAll(filepath.Dir(h.Path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(h.Path, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

// Callback returns an ssh.HostKeyCallback bound to a tab (for prompt routing).
func (h *HostKeys) Callback(ctx context.Context, tabID string) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		h.mu.Lock()
		defer h.mu.Unlock()
		if err := h.ensure(); err != nil {
			return err
		}
		cb, err := knownhosts.New(h.Path)
		if err != nil {
			return fmt.Errorf("known_hosts: %w", err)
		}
		err = cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) {
			return err
		}
		fp := ssh.FingerprintSHA256(key)
		if len(ke.Want) > 0 {
			if h.Prompt != nil {
				go func() {
					_, _ = h.Prompt.Ask(context.Background(), prompt.Request{
						Kind: prompt.KindHostKeyBad, TabID: tabID,
						Title: "WARNING: host key changed",
						Message: fmt.Sprintf("The host key for %s has changed (now %s %s).\n"+
							"This may be a man-in-the-middle attack, or the device was re-installed.\n"+
							"Connection refused. If the change is expected, remove the old key in Settings → Known hosts (line %d of %s).",
							hostname, key.Type(), fp, ke.Want[0].Line, h.Path),
						Fingerprint: fp,
						Params: map[string]string{
							"host": hostname, "keyType": key.Type(),
							"line": strconv.Itoa(ke.Want[0].Line), "path": h.Path,
						},
					})
				}()
			}
			return fmt.Errorf("%w for %s (%s %s)", ErrHostKeyChanged, hostname, key.Type(), fp)
		}
		if h.Prompt == nil {
			return fmt.Errorf("unknown host key for %s", hostname)
		}
		_, err = h.Prompt.Ask(ctx, prompt.Request{
			Kind: prompt.KindHostKey, TabID: tabID,
			Title:       "Unknown host",
			Message:     fmt.Sprintf("The authenticity of host %s can't be established.\n%s key fingerprint is:", hostname, key.Type()),
			Fingerprint: fp,
			Params:      map[string]string{"host": hostname, "keyType": key.Type()},
		})
		if err != nil {
			return fmt.Errorf("host key not accepted for %s", hostname)
		}
		return h.add(hostname, remote, key)
	}
}

func (h *HostKeys) add(hostname string, remote net.Addr, key ssh.PublicKey) error {
	f, err := os.OpenFile(h.Path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	addrs := []string{knownhosts.Normalize(hostname)}
	_, err = fmt.Fprintln(f, knownhosts.Line(addrs, key))
	return err
}

// Entry is one known_hosts line for the UI.
type Entry struct {
	Line        int    `json:"line"`
	Hosts       string `json:"hosts"`
	KeyType     string `json:"keyType"`
	Fingerprint string `json:"fingerprint"`
}

// List returns parsed entries.
func (h *HostKeys) List() ([]Entry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f, err := os.Open(h.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		_, hosts, key, _, _, err := ssh.ParseKnownHosts([]byte(line))
		if err != nil {
			continue
		}
		out = append(out, Entry{Line: n, Hosts: strings.Join(hosts, ","), KeyType: key.Type(), Fingerprint: ssh.FingerprintSHA256(key)})
	}
	return out, sc.Err()
}

// RemoveLine deletes the given 1-based line.
func (h *HostKeys) RemoveLine(line int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, err := os.ReadFile(h.Path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	if line < 1 || line > len(lines) {
		return errors.New("no such line")
	}
	lines = append(lines[:line-1], lines[line:]...)
	return os.WriteFile(h.Path, []byte(strings.Join(lines, "\n")), 0o600)
}

var (
	probeKeyOnce sync.Once
	probeKey     ssh.PublicKey
)

func getProbeKey() ssh.PublicKey {
	probeKeyOnce.Do(func() {
		pub, _, _ := ed25519.GenerateKey(rand.Reader)
		probeKey, _ = ssh.NewPublicKey(pub)
	})
	return probeKey
}

// Algorithms returns the host key algorithms already recorded for addr, so
// the server is asked for a key type we can verify (avoids a false
// "changed" error when a host offers several key types).
func (h *HostKeys) Algorithms(addr string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	cb, err := knownhosts.New(h.Path)
	if err != nil {
		return nil
	}
	err = cb(addr, &net.TCPAddr{IP: net.IPv4zero}, getProbeKey())
	var ke *knownhosts.KeyError
	if !errors.As(err, &ke) || len(ke.Want) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(a ...string) {
		for _, x := range a {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	for _, k := range ke.Want {
		switch t := k.Key.Type(); t {
		case ssh.KeyAlgoRSA:
			add(ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA)
		default:
			add(t)
		}
	}
	return out
}
