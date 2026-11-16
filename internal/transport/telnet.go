package transport

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

// Telnet protocol bytes.
const (
	tnSE   = 240
	tnNOP  = 241
	tnSB   = 250
	tnWILL = 251
	tnWONT = 252
	tnDO   = 253
	tnDONT = 254
	tnIAC  = 255

	optBinary = 0
	optEcho   = 1
	optSGA    = 3
	optTType  = 24
	optNAWS   = 31

	ttypeIS   = 0
	ttypeSEND = 1
)

type tnState int

const (
	stData tnState = iota
	stIAC
	stCmd
	stSB
	stSBIAC
)

// Telnet is a telnet client with minimal option negotiation
// (ECHO, SGA, BINARY from the server; NAWS and TTYPE from us).
type Telnet struct {
	Host string
	Port int
	Cols int
	Rows int

	conn net.Conn
	wmu  sync.Mutex // serializes writes to conn

	// parser state (used only by Read)
	rbuf  []byte
	state tnState
	cmd   byte
	sb    []byte
	cr    bool

	mu      sync.Mutex
	remote  map[byte]bool // options enabled on server side (WILL acknowledged)
	local   map[byte]bool // options enabled on our side (DO acknowledged)
	refused map[[2]byte]bool
}

func (t *Telnet) Open(ctx context.Context) error {
	port := t.Port
	if port == 0 {
		port = 23
	}
	d := net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(t.Host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	t.mu.Lock()
	t.conn = c
	t.remote = map[byte]bool{}
	t.local = map[byte]bool{}
	t.refused = map[[2]byte]bool{}
	t.mu.Unlock()
	t.rbuf = make([]byte, 16*1024)
	return nil
}

func (t *Telnet) send(b []byte) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	if t.conn == nil {
		return ErrClosed
	}
	_, err := t.conn.Write(b)
	return err
}

func (t *Telnet) Read(p []byte) (int, error) {
	if t.conn == nil {
		return 0, ErrClosed
	}
	for {
		max := min(len(p), len(t.rbuf))
		n, err := t.conn.Read(t.rbuf[:max])
		out := t.parse(t.rbuf[:n], p[:0])
		if len(out) > 0 || err != nil {
			return len(out), err
		}
	}
}

// parse feeds raw bytes through the IAC state machine, appending data bytes
// to dst (which never grows beyond len(src)).
func (t *Telnet) parse(src, dst []byte) []byte {
	for _, b := range src {
		switch t.state {
		case stData:
			if t.cr {
				t.cr = false
				if b == 0 {
					continue
				}
			}
			if b == tnIAC {
				t.state = stIAC
				continue
			}
			if b == '\r' {
				t.cr = true
			}
			dst = append(dst, b)
		case stIAC:
			switch b {
			case tnIAC:
				dst = append(dst, tnIAC)
				t.state = stData
			case tnWILL, tnWONT, tnDO, tnDONT:
				t.cmd = b
				t.state = stCmd
			case tnSB:
				t.sb = t.sb[:0]
				t.state = stSB
			default: // NOP, GA, etc.
				t.state = stData
			}
		case stCmd:
			t.negotiate(t.cmd, b)
			t.state = stData
		case stSB:
			if b == tnIAC {
				t.state = stSBIAC
			} else if len(t.sb) < 4096 {
				t.sb = append(t.sb, b)
			}
		case stSBIAC:
			switch b {
			case tnSE:
				t.subneg(t.sb)
				t.state = stData
			case tnIAC:
				t.sb = append(t.sb, tnIAC)
				t.state = stSB
			default:
				t.state = stSB
			}
		}
	}
	return dst
}

func (t *Telnet) reply(cmd, opt byte) {
	_ = t.send([]byte{tnIAC, cmd, opt})
}

func (t *Telnet) negotiate(cmd, opt byte) {
	t.mu.Lock()
	var replyCmd byte
	sendNAWS := false
	switch cmd {
	case tnWILL:
		switch opt {
		case optEcho, optSGA, optBinary:
			if !t.remote[opt] {
				t.remote[opt] = true
				replyCmd = tnDO
			}
		default:
			k := [2]byte{tnDONT, opt}
			if !t.refused[k] {
				t.refused[k] = true
				replyCmd = tnDONT
			}
		}
	case tnWONT:
		if t.remote[opt] {
			t.remote[opt] = false
			replyCmd = tnDONT
		}
	case tnDO:
		switch opt {
		case optNAWS, optTType, optSGA, optBinary:
			if !t.local[opt] {
				t.local[opt] = true
				replyCmd = tnWILL
			}
			if opt == optNAWS {
				sendNAWS = true
			}
		default:
			k := [2]byte{tnWONT, opt}
			if !t.refused[k] {
				t.refused[k] = true
				replyCmd = tnWONT
			}
		}
	case tnDONT:
		if t.local[opt] {
			t.local[opt] = false
			replyCmd = tnWONT
		}
	}
	t.mu.Unlock()
	if replyCmd != 0 {
		t.reply(replyCmd, opt)
	}
	if sendNAWS {
		t.sendNAWS()
	}
}

func (t *Telnet) subneg(sb []byte) {
	if len(sb) >= 2 && sb[0] == optTType && sb[1] == ttypeSEND {
		msg := []byte{tnIAC, tnSB, optTType, ttypeIS}
		msg = append(msg, "XTERM-256COLOR"...)
		msg = append(msg, tnIAC, tnSE)
		_ = t.send(msg)
	}
}

func (t *Telnet) sendNAWS() {
	cols, rows := t.Cols, t.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	msg := []byte{tnIAC, tnSB, optNAWS}
	for _, v := range []int{cols, rows} {
		hi, lo := byte(v>>8), byte(v)
		msg = append(msg, hi)
		if hi == tnIAC {
			msg = append(msg, tnIAC)
		}
		msg = append(msg, lo)
		if lo == tnIAC {
			msg = append(msg, tnIAC)
		}
	}
	msg = append(msg, tnIAC, tnSE)
	_ = t.send(msg)
}

// encodeOut escapes IAC and turns a lone CR into CR LF (Telnet newline).
func encodeOut(p []byte, binary bool) []byte {
	var b bytes.Buffer
	b.Grow(len(p) + 4)
	for i, c := range p {
		switch {
		case c == tnIAC:
			b.WriteByte(tnIAC)
			b.WriteByte(tnIAC)
		case c == '\r' && !binary && (i+1 >= len(p) || p[i+1] != '\n'):
			b.WriteString("\r\n")
		default:
			b.WriteByte(c)
		}
	}
	return b.Bytes()
}

func (t *Telnet) Write(p []byte) (int, error) {
	t.mu.Lock()
	bin := t.local[optBinary]
	t.mu.Unlock()
	if err := t.send(encodeOut(p, bin)); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *Telnet) Resize(cols, rows int) error {
	t.mu.Lock()
	t.Cols, t.Rows = cols, rows
	naws := t.local[optNAWS]
	t.mu.Unlock()
	if naws {
		t.sendNAWS()
	}
	return nil
}

func (t *Telnet) Close() error {
	t.wmu.Lock()
	c := t.conn
	t.wmu.Unlock()
	if c != nil {
		return c.Close()
	}
	return nil
}

// Describe implements Info.
func (t *Telnet) Describe() string {
	port := t.Port
	if port == 0 {
		port = 23
	}
	return fmt.Sprintf("Telnet %s:%d", t.Host, port)
}
