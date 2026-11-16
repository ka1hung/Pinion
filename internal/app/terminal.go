package app

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ka1hung/pinion/internal/charset"
	"github.com/ka1hung/pinion/internal/store"
	"github.com/ka1hung/pinion/internal/tabs"
	"github.com/ka1hung/pinion/internal/transport"
)

// TerminalService opens and controls terminal tabs.
type TerminalService struct{ c *Core }

// NewTerminalService creates the service.
func NewTerminalService(c *Core) *TerminalService { return &TerminalService{c: c} }

// StreamInfo tells the frontend where the local WebSocket is.
type StreamInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// StreamInfo returns the WebSocket port and per-launch token.
func (t *TerminalService) StreamInfo() StreamInfo {
	return StreamInfo{Port: t.c.Hub.Port(), Token: t.c.Hub.Token()}
}

// Open opens a saved session in a new tab.
func (t *TerminalService) Open(sessionID string, cols, rows int) (*tabs.Info, error) {
	s, err := t.c.Store.Get(sessionID)
	if err != nil {
		return nil, err
	}
	return t.c.Tabs.Open(s, cols, rows)
}

// OpenAdhoc opens an unsaved session (quick connect).
func (t *TerminalService) OpenAdhoc(sess store.Session, cols, rows int) (*tabs.Info, error) {
	if sess.Name == "" {
		switch {
		case sess.SSH != nil:
			sess.Name = sess.SSH.Host
		case sess.Telnet != nil:
			sess.Name = sess.Telnet.Host
		case sess.Serial != nil:
			sess.Name = sess.Serial.Port
		case sess.Net != nil:
			sess.Name = fmt.Sprintf("%s %s:%d", strings.ToUpper(sess.Type), sess.Net.Host, sess.Net.Port)
		default:
			sess.Name = sess.Type
		}
	}
	sess.ID = ""
	return t.c.Tabs.Open(&sess, cols, rows)
}

// OpenLocal opens a local shell.
func (t *TerminalService) OpenLocal(cols, rows int) (*tabs.Info, error) {
	return t.c.Tabs.Open(&store.Session{Name: "Local", Type: store.TypeLocal}, cols, rows)
}

// Close closes a tab.
func (t *TerminalService) Close(id string) error { return t.c.Tabs.Close(id) }

// Reconnect reconnects a tab.
func (t *TerminalService) Reconnect(id string) error { return t.c.Tabs.Reconnect(id) }

// List returns open tabs.
func (t *TerminalService) List() []*tabs.Info { return t.c.Tabs.List() }

// Broadcast sends text to several tabs at once (MultiExec).
func (t *TerminalService) Broadcast(ids []string, data string) {
	t.c.Tabs.Broadcast(ids, []byte(data))
}

// ParsePayload converts a send-box payload to bytes. format: text|hex|json.
func ParsePayload(data, format string) ([]byte, error) {
	switch format {
	case "hex":
		s := strings.NewReplacer("0x", "", "0X", "", ",", " ", "\n", " ", "\r", " ", "\t", " ").Replace(data)
		s = strings.Join(strings.Fields(s), "")
		if len(s)%2 == 1 {
			return nil, errors.New("hex: odd number of digits")
		}
		return hex.DecodeString(s)
	case "json":
		if !json.Valid([]byte(data)) {
			var v any
			err := json.Unmarshal([]byte(data), &v)
			return nil, fmt.Errorf("invalid JSON: %v", err)
		}
		var buf bytes.Buffer
		if err := json.Compact(&buf, []byte(data)); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return []byte(data), nil
	}
}

// Send writes a composed message (TCP/UDP/serial send box).
// appendLine: none | cr | lf | crlf.
func (t *TerminalService) Send(id, data, format, appendLine string) error {
	tab, err := t.c.Tabs.Get(id)
	if err != nil {
		return err
	}
	b, err := ParsePayload(data, format)
	if err != nil {
		return err
	}
	if format != "hex" {
		b = charset.Encode(tab.Session().Charset, b)
	}
	switch appendLine {
	case "cr":
		b = append(b, '\r')
	case "lf":
		b = append(b, '\n')
	case "crlf":
		b = append(b, '\r', '\n')
	}
	return tab.Send(b)
}

// SetLogging turns the session log on/off; returns the log file path.
func (t *TerminalService) SetLogging(id string, on bool) (string, error) {
	return t.c.Tabs.SetLogging(id, on)
}

// LineControl toggles serial DTR/RTS or sends BREAK. op: dtr|rts|break.
func (t *TerminalService) LineControl(id, op string, on bool) error {
	tab, err := t.c.Tabs.Get(id)
	if err != nil {
		return err
	}
	lc, ok := tab.Transport().(transport.LineControl)
	if !ok {
		return errors.New("not a serial port")
	}
	switch op {
	case "dtr":
		return lc.SetDTR(on)
	case "rts":
		return lc.SetRTS(on)
	case "break":
		return lc.SendBreak()
	}
	return errors.New("unknown op")
}

// ClearScrollback forgets the replay buffer of a tab.
func (t *TerminalService) ClearScrollback(id string) { t.c.Tabs.ClearScrollback(id) }

// SaveOutput writes text (e.g. the visible buffer) to a file the user picks.
func (t *TerminalService) SaveOutput(suggestedName, text string) (string, error) {
	p, err := t.c.PickSave("Save terminal output", suggestedName, []FileFilter{{"Text", "*.txt;*.log"}})
	if err != nil || p == "" {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, []byte(text), 0o600)
}
