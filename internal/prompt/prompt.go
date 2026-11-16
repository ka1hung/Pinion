// Package prompt lets backend code synchronously ask the user something
// (host key confirmation, password, keyboard-interactive answers). A request
// is emitted as a UI event and the call blocks until the UI answers via
// Broker.Answer or the context is cancelled.
package prompt

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
)

// Kinds.
const (
	KindHostKey    = "hostkey"    // yes/no with fingerprint
	KindHostKeyBad = "hostkeybad" // changed key: info only, always rejected
	KindPassword   = "password"
	KindLogin      = "login" // username (+ password) when the session has no user
	KindPassphrase = "passphrase"
	KindKbdInt     = "kbdint"
	KindConfirm    = "confirm"
	KindMaster     = "master" // fallback vault master password
)

// Field is one input of a prompt.
type Field struct {
	Label string `json:"label"`
	Echo  bool   `json:"echo"`
}

// Request is sent to the UI as the "prompt" event.
type Request struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Title       string  `json:"title"`
	Message     string  `json:"message"`
	Fields      []Field `json:"fields,omitempty"`
	Fingerprint string  `json:"fingerprint,omitempty"`
	CanSave     bool    `json:"canSave,omitempty"`
	// Params carries the values the UI needs to build its localized text
	// (Title/Message/Field labels are English fallbacks).
	Params map[string]string `json:"params,omitempty"`
	TabID  string            `json:"tabId,omitempty"`
}

// Response comes back from the UI.
type Response struct {
	ID      string   `json:"id"`
	OK      bool     `json:"ok"`
	Answers []string `json:"answers"`
	Save    bool     `json:"save"`
}

// ErrCancelled is returned when the user dismisses the prompt.
var ErrCancelled = errors.New("cancelled by user")

// Emitter sends an event to the UI.
type Emitter func(name string, data any)

// Broker routes prompts.
type Broker struct {
	emit    Emitter
	seq     atomic.Uint64
	mu      sync.Mutex
	pending map[string]chan Response
}

// NewBroker creates a broker.
func NewBroker(emit Emitter) *Broker {
	return &Broker{emit: emit, pending: map[string]chan Response{}}
}

// Ask blocks until the user answers.
func (b *Broker) Ask(ctx context.Context, req Request) (Response, error) {
	req.ID = "p" + strconv.FormatUint(b.seq.Add(1), 10)
	ch := make(chan Response, 1)
	b.mu.Lock()
	b.pending[req.ID] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, req.ID)
		b.mu.Unlock()
	}()
	if b.emit != nil {
		b.emit("prompt", req)
	}
	select {
	case r := <-ch:
		if !r.OK {
			return r, ErrCancelled
		}
		return r, nil
	case <-ctx.Done():
		if b.emit != nil {
			b.emit("prompt:cancel", req.ID)
		}
		return Response{}, ctx.Err()
	}
}

// Answer delivers a response from the UI.
func (b *Broker) Answer(r Response) {
	b.mu.Lock()
	ch := b.pending[r.ID]
	b.mu.Unlock()
	if ch != nil {
		select {
		case ch <- r:
		default:
		}
	}
}
