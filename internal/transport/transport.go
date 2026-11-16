// Package transport defines the single interface every connection protocol
// (SSH, Serial, Telnet, local shell, TCP/UDP) implements. Tabs, logging,
// MultiExec and reconnect logic are written once against this interface.
package transport

import (
	"context"
	"errors"
	"io"
)

// Transport is a bidirectional byte stream with an optional terminal size.
//
// Lifecycle: Open is called exactly once before any Read/Write. Read blocks
// until data is available or the transport is closed (io.EOF). Close may be
// called concurrently with Read/Write and must unblock them.
type Transport interface {
	Open(ctx context.Context) error
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	Close() error
}

// Info is optional metadata a transport can expose for the UI status bar.
type Info interface {
	Describe() string
}

// ErrClosed is returned by Read/Write after Close.
var ErrClosed = errors.New("transport closed")

// IsEOF reports whether err indicates a normal end of stream.
func IsEOF(err error) bool {
	return err == nil || errors.Is(err, io.EOF) || errors.Is(err, ErrClosed)
}

// Notifier is implemented by transports that report out-of-band events
// (e.g. "client 10.0.0.5:5000 connected" for a TCP server). Notices are shown
// in the terminal but bypass HEX/timestamp transforms and charset decoding.
type Notifier interface {
	Notices() <-chan string
}

// LineControl is implemented by serial ports.
type LineControl interface {
	SetDTR(on bool) error
	SetRTS(on bool) error
	SendBreak() error
}
