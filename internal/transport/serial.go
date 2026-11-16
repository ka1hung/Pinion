package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"

	"github.com/ka1hung/pinion/internal/serialx"
	"github.com/ka1hung/pinion/internal/store"
)

// Serial is a serial (COM / tty) port.
type Serial struct {
	Config *store.SerialConfig

	mu     sync.Mutex
	port   serial.Port
	closed bool
}

func serialMode(c *store.SerialConfig) (*serial.Mode, error) {
	m := &serial.Mode{BaudRate: c.Baud, DataBits: c.DataBits}
	if m.BaudRate <= 0 {
		m.BaudRate = 115200
	}
	if m.DataBits == 0 {
		m.DataBits = 8
	}
	if m.DataBits < 5 || m.DataBits > 8 {
		return nil, fmt.Errorf("invalid data bits %d", m.DataBits)
	}
	switch strings.ToLower(c.Parity) {
	case "", "none", "n":
		m.Parity = serial.NoParity
	case "odd", "o":
		m.Parity = serial.OddParity
	case "even", "e":
		m.Parity = serial.EvenParity
	case "mark", "m":
		m.Parity = serial.MarkParity
	case "space", "s":
		m.Parity = serial.SpaceParity
	default:
		return nil, fmt.Errorf("invalid parity %q", c.Parity)
	}
	switch c.StopBits {
	case "", "1":
		m.StopBits = serial.OneStopBit
	case "1.5":
		m.StopBits = serial.OnePointFiveStopBits
	case "2":
		m.StopBits = serial.TwoStopBits
	default:
		return nil, fmt.Errorf("invalid stop bits %q", c.StopBits)
	}
	dtr, rts := true, true
	if c.DTR != nil {
		dtr = *c.DTR
	}
	if c.RTS != nil {
		rts = *c.RTS
	}
	m.InitialStatusBits = &serial.ModemOutputBits{DTR: dtr, RTS: rts}
	return m, nil
}

func (t *Serial) Open(ctx context.Context) error {
	if t.Config == nil || t.Config.Port == "" {
		return errors.New("no serial port selected")
	}
	mode, err := serialMode(t.Config)
	if err != nil {
		return err
	}
	type res struct {
		p   serial.Port
		err error
	}
	ch := make(chan res, 1)
	go func() {
		p, err := serial.Open(t.Config.Port, mode)
		if err != nil && mode.InitialStatusBits != nil && strings.Contains(err.Error(), "modem bits") {
			// Virtual ports (pty, com0com, some USB gateways) don't support
			// modem-control ioctls: open again without touching DTR/RTS.
			m2 := *mode
			m2.InitialStatusBits = nil
			p, err = serial.Open(t.Config.Port, &m2)
		}
		ch <- res{p, err}
	}()
	var r res
	select {
	case r = <-ch:
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.p != nil {
				_ = r.p.Close()
			}
		}()
		return ctx.Err()
	}
	if r.err != nil {
		return errors.New(serialx.OpenErrorHint(runtime.GOOS, t.Config.Port, r.err))
	}
	_ = r.p.SetReadTimeout(serial.NoTimeout)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		_ = r.p.Close()
		return ErrClosed
	}
	t.port = r.p
	return nil
}

func (t *Serial) get() serial.Port {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.port
}

func (t *Serial) Read(p []byte) (int, error) {
	port := t.get()
	if port == nil {
		return 0, ErrClosed
	}
	for {
		n, err := port.Read(p)
		if err != nil {
			t.mu.Lock()
			closed := t.closed
			t.mu.Unlock()
			if closed {
				return n, io.EOF
			}
			var pe *serial.PortError
			if errors.As(err, &pe) && pe.Code() == serial.PortClosed {
				return n, io.EOF
			}
			return n, fmt.Errorf("serial port %s: %w", t.Config.Port, err)
		}
		if n == 0 {
			// Some drivers return 0 bytes when the device is unplugged.
			t.mu.Lock()
			closed := t.closed
			t.mu.Unlock()
			if closed {
				return 0, io.EOF
			}
			if _, err := port.GetModemStatusBits(); err != nil {
				return 0, fmt.Errorf("serial port %s disconnected: %w", t.Config.Port, err)
			}
			continue
		}
		return n, nil
	}
}

func (t *Serial) Write(p []byte) (int, error) {
	port := t.get()
	if port == nil {
		return 0, ErrClosed
	}
	return port.Write(p)
}

func (t *Serial) Resize(cols, rows int) error { return nil }

func (t *Serial) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	port := t.port
	t.mu.Unlock()
	if port != nil {
		return port.Close()
	}
	return nil
}

// SetDTR implements LineControl.
func (t *Serial) SetDTR(on bool) error {
	port := t.get()
	if port == nil {
		return ErrClosed
	}
	return port.SetDTR(on)
}

// SetRTS implements LineControl.
func (t *Serial) SetRTS(on bool) error {
	port := t.get()
	if port == nil {
		return ErrClosed
	}
	return port.SetRTS(on)
}

// SendBreak implements LineControl.
func (t *Serial) SendBreak() error {
	port := t.get()
	if port == nil {
		return ErrClosed
	}
	return port.Break(250 * time.Millisecond)
}

// Describe implements Info, e.g. "Serial COM3 115200 8N1".
func (t *Serial) Describe() string {
	c := t.Config
	if c == nil {
		return "Serial"
	}
	par := "N"
	if c.Parity != "" {
		par = strings.ToUpper(c.Parity[:1])
	}
	db := c.DataBits
	if db == 0 {
		db = 8
	}
	sb := c.StopBits
	if sb == "" {
		sb = "1"
	}
	return fmt.Sprintf("Serial %s %d %d%s%s", c.Port, c.Baud, db, par, sb)
}
