// Package sshx builds SSH connections for saved sessions: auth from the
// keychain / agent / key files with interactive fallback prompts, strict
// known_hosts verification, ProxyJump chains and keepalive.
package sshx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/ka1hung/pinion/internal/prompt"
	"github.com/ka1hung/pinion/internal/secret"
	"github.com/ka1hung/pinion/internal/store"
)

// Dialer creates SSH clients.
type Dialer struct {
	Store   *store.Store
	Secrets *secret.Store
	Prompt  *prompt.Broker
	hk      *HostKeys
}

// NewDialer creates a dialer.
func NewDialer(st *store.Store, sec *secret.Store, pr *prompt.Broker) *Dialer {
	return &Dialer{Store: st, Secrets: sec, Prompt: pr, hk: &HostKeys{Path: st.KnownHostsPath(), Prompt: pr}}
}

// HostKeys exposes the known_hosts manager.
func (d *Dialer) HostKeys() *HostKeys { return d.hk }

// Target is a resolved hop.
type Target struct {
	SessionID string // for keychain lookups; may be empty for inline hops
	Name      string
	Host      string
	Port      int
	User      string
	Auth      string
	KeyPath   string
}

func (t Target) addr() string { return net.JoinHostPort(t.Host, strconv.Itoa(t.Port)) }

// Conn is a live SSH connection (possibly through jump hosts).
type Conn struct {
	Client  *ssh.Client
	Target  Target
	closers []func() error
	done    chan struct{}
	once    sync.Once
	err     error
}

// Done is closed when the connection dies.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err returns why the connection ended.
func (c *Conn) Err() error { return c.err }

// Close closes the connection and its jump hops.
func (c *Conn) Close() error {
	c.finish(nil)
	return nil
}

func (c *Conn) finish(err error) {
	c.once.Do(func() {
		c.err = err
		_ = c.Client.Close()
		for i := len(c.closers) - 1; i >= 0; i-- {
			_ = c.closers[i]()
		}
		close(c.done)
	})
}

// Dial connects to a saved SSH session, through its jump hosts.
func (d *Dialer) Dial(ctx context.Context, sess *store.Session, tabID string) (*Conn, error) {
	if sess.SSH == nil {
		return nil, errors.New("not an SSH session")
	}
	hops, err := d.resolveChain(sess, map[string]bool{})
	if err != nil {
		return nil, err
	}
	var (
		client  *ssh.Client
		closers []func() error
	)
	cleanup := func() {
		for i := len(closers) - 1; i >= 0; i-- {
			_ = closers[i]()
		}
	}
	for i, hop := range hops {
		cfg, saver := d.clientConfig(ctx, hop, tabID)
		var c *ssh.Client
		if client == nil {
			c, err = dialDirect(ctx, hop.addr(), cfg)
		} else {
			c, err = dialVia(ctx, client, hop.addr(), cfg)
		}
		if err != nil {
			cleanup()
			if i < len(hops)-1 {
				return nil, fmt.Errorf("jump host %s: %w", hop.addr(), err)
			}
			return nil, err
		}
		saver.commit()
		if i < len(hops)-1 {
			closers = append(closers, c.Close)
		}
		client = c
	}
	conn := &Conn{Client: client, Target: hops[len(hops)-1], closers: closers, done: make(chan struct{})}
	go func() {
		err := client.Wait()
		conn.finish(err)
	}()
	ka := sess.SSH.KeepAlive
	if ka <= 0 {
		ka = 30
	}
	go keepalive(conn, time.Duration(ka)*time.Second)
	return conn, nil
}

func keepalive(c *Conn, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	misses := 0
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			res := make(chan error, 1)
			go func() {
				_, _, err := c.Client.SendRequest("keepalive@openssh.com", true, nil)
				res <- err
			}()
			select {
			case err := <-res:
				if err != nil {
					c.finish(fmt.Errorf("keepalive failed: %w", err))
					return
				}
				misses = 0
			case <-time.After(every):
				misses++
				if misses >= 3 {
					c.finish(errors.New("keepalive timeout"))
					return
				}
			case <-c.done:
				return
			}
		}
	}
}

func dialDirect(ctx context.Context, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	var nd net.Dialer
	nc, err := nd.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return handshake(ctx, nc, addr, cfg)
}

func dialVia(ctx context.Context, via *ssh.Client, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	nc, err := via.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return handshake(ctx, nc, addr, cfg)
}

func handshake(ctx context.Context, nc net.Conn, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })
	defer stop()
	cc, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	if err != nil {
		_ = nc.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return ssh.NewClient(cc, chans, reqs), nil
}

func (d *Dialer) resolveChain(sess *store.Session, seen map[string]bool) ([]Target, error) {
	if sess.ID != "" {
		if seen[sess.ID] {
			return nil, errors.New("ProxyJump loop detected")
		}
		seen[sess.ID] = true
	}
	var out []Target
	for _, j := range sess.SSH.JumpHosts {
		if j.SessionID != "" {
			js, err := d.Store.Get(j.SessionID)
			if err != nil {
				return nil, fmt.Errorf("jump host session: %w", err)
			}
			if js.SSH == nil {
				return nil, fmt.Errorf("jump host %q is not an SSH session", js.Name)
			}
			sub, err := d.resolveChain(js, seen)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
			continue
		}
		port := j.Port
		if port == 0 {
			port = 22
		}
		out = append(out, Target{Name: j.Host, Host: j.Host, Port: port, User: j.User, Auth: store.AuthAuto})
	}
	out = append(out, Target{
		SessionID: sess.ID, Name: sess.Name, Host: sess.SSH.Host, Port: sess.SSH.Port,
		User: sess.SSH.User, Auth: sess.SSH.Auth, KeyPath: sess.SSH.KeyPath,
	})
	return out, nil
}

// pendingSave holds secrets the user asked to remember; they are written to
// the keychain only after authentication succeeded.
type pendingSave struct {
	d    *Dialer
	mu   sync.Mutex
	vals map[string]string
}

func (p *pendingSave) set(k, v string) {
	p.mu.Lock()
	p.vals[k] = v
	p.mu.Unlock()
}

func (p *pendingSave) commit() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, v := range p.vals {
		_ = p.d.Secrets.Set(k, v)
	}
}

func (d *Dialer) clientConfig(ctx context.Context, t Target, tabID string) (*ssh.ClientConfig, *pendingSave) {
	ps := &pendingSave{d: d, vals: map[string]string{}}
	user := t.User
	if user == "" {
		user = os.Getenv("USER")
		if user == "" {
			user = os.Getenv("USERNAME")
		}
	}
	var methods []ssh.AuthMethod
	auth := t.Auth
	if auth == "" {
		auth = store.AuthAuto
	}
	if auth == store.AuthAgent || auth == store.AuthAuto {
		if m := agentAuth(); m != nil {
			methods = append(methods, m)
		}
	}
	if auth == store.AuthKey || auth == store.AuthAuto {
		var paths []string
		if t.KeyPath != "" {
			paths = append(paths, expandHome(t.KeyPath))
		} else if auth == store.AuthAuto {
			paths = defaultKeyPaths()
		}
		if len(paths) > 0 {
			methods = append(methods, ssh.PublicKeysCallback(func() ([]ssh.Signer, error) {
				return d.loadSigners(ctx, t, tabID, paths, ps, t.KeyPath != "")
			}))
		}
	}
	if auth == store.AuthPassword || auth == store.AuthAuto {
		pw := d.passwordSource(ctx, t, user, tabID, ps)
		methods = append(methods,
			ssh.RetryableAuthMethod(ssh.PasswordCallback(pw), 3),
			ssh.RetryableAuthMethod(ssh.KeyboardInteractive(d.kbdInt(ctx, t, user, tabID, pw)), 3),
		)
	}
	return &ssh.ClientConfig{
		User:              user,
		Auth:              methods,
		HostKeyCallback:   d.hk.Callback(ctx, tabID),
		HostKeyAlgorithms: d.hk.Algorithms(t.addr()),
		Timeout:           20 * time.Second,
		ClientVersion:     "SSH-2.0-Pinion",
	}, ps
}

func (d *Dialer) passwordSource(ctx context.Context, t Target, user, tabID string, ps *pendingSave) func() (string, error) {
	triedSaved := false
	return func() (string, error) {
		if !triedSaved && t.SessionID != "" {
			triedSaved = true
			if v, err := d.Secrets.Get(secret.Key(t.SessionID, secret.KindPassword)); err == nil && v != "" {
				return v, nil
			}
		}
		triedSaved = true
		r, err := d.Prompt.Ask(ctx, prompt.Request{
			Kind: prompt.KindPassword, TabID: tabID,
			Title:   "Password",
			Message: fmt.Sprintf("%s@%s", user, t.addr()),
			Fields:  []prompt.Field{{Label: "Password"}},
			CanSave: t.SessionID != "",
		})
		if err != nil {
			return "", err
		}
		pw := ""
		if len(r.Answers) > 0 {
			pw = r.Answers[0]
		}
		if r.Save && t.SessionID != "" {
			ps.set(secret.Key(t.SessionID, secret.KindPassword), pw)
		}
		return pw, nil
	}
}

func (d *Dialer) kbdInt(ctx context.Context, t Target, user, tabID string, pw func() (string, error)) ssh.KeyboardInteractiveChallenge {
	return func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		if len(questions) == 0 {
			return nil, nil
		}
		// Single hidden "Password:" question: reuse password flow (saved/remember).
		if len(questions) == 1 && !echos[0] && strings.Contains(strings.ToLower(questions[0]), "password") {
			v, err := pw()
			if err != nil {
				return nil, err
			}
			return []string{v}, nil
		}
		fields := make([]prompt.Field, len(questions))
		for i, q := range questions {
			fields[i] = prompt.Field{Label: strings.TrimSpace(q), Echo: echos[i]}
		}
		msg := strings.TrimSpace(name + "\n" + instruction)
		if msg == "" {
			msg = fmt.Sprintf("%s@%s", user, t.addr())
		}
		r, err := d.Prompt.Ask(ctx, prompt.Request{
			Kind: prompt.KindKbdInt, TabID: tabID, Title: "Authentication", Message: msg, Fields: fields,
		})
		if err != nil {
			return nil, err
		}
		ans := make([]string, len(questions))
		copy(ans, r.Answers)
		return ans, nil
	}
}

func (d *Dialer) loadSigners(ctx context.Context, t Target, tabID string, paths []string, ps *pendingSave, explicit bool) ([]ssh.Signer, error) {
	var out []ssh.Signer
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			if explicit {
				return nil, fmt.Errorf("read key %s: %w", p, err)
			}
			continue
		}
		s, err := ssh.ParsePrivateKey(b)
		var pme *ssh.PassphraseMissingError
		if errors.As(err, &pme) {
			s, err = d.decryptKey(ctx, t, tabID, p, b, ps)
		}
		if err != nil {
			if explicit {
				return nil, fmt.Errorf("key %s: %w", p, err)
			}
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (d *Dialer) decryptKey(ctx context.Context, t Target, tabID, path string, pem []byte, ps *pendingSave) (ssh.Signer, error) {
	if t.SessionID != "" {
		if v, err := d.Secrets.Get(secret.Key(t.SessionID, secret.KindPassphrase)); err == nil && v != "" {
			if s, err := ssh.ParsePrivateKeyWithPassphrase(pem, []byte(v)); err == nil {
				return s, nil
			}
		}
	}
	for range 3 {
		r, err := d.Prompt.Ask(ctx, prompt.Request{
			Kind: prompt.KindPassphrase, TabID: tabID, Title: "Key passphrase",
			Message: filepath.Base(path), Fields: []prompt.Field{{Label: "Passphrase"}},
			CanSave: t.SessionID != "",
		})
		if err != nil {
			return nil, err
		}
		pass := ""
		if len(r.Answers) > 0 {
			pass = r.Answers[0]
		}
		s, err := ssh.ParsePrivateKeyWithPassphrase(pem, []byte(pass))
		if err == nil {
			if r.Save && t.SessionID != "" {
				ps.set(secret.Key(t.SessionID, secret.KindPassphrase), pass)
			}
			return s, nil
		}
	}
	return nil, errors.New("wrong passphrase")
}

func agentAuth() ssh.AuthMethod {
	conn, err := dialAgent()
	if err != nil || conn == nil {
		return nil
	}
	ag := agent.NewClient(conn)
	return ssh.PublicKeysCallback(ag.Signers)
}

// AgentAvailable reports whether an SSH agent is reachable.
func AgentAvailable() bool {
	c, err := dialAgent()
	if err != nil || c == nil {
		return false
	}
	_ = c.Close()
	return true
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

func defaultKeyPaths() []string {
	h, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []string
	for _, n := range []string{"id_ed25519", "id_ecdsa", "id_rsa"} {
		p := filepath.Join(h, ".ssh", n)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}
