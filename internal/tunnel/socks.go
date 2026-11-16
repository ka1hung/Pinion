package tunnel

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// SOCKS5 reply codes.
const (
	socksOK              = 0x00
	socksGeneralFailure  = 0x01
	socksNetUnreachable  = 0x03
	socksHostUnreachable = 0x04
	socksConnRefused     = 0x05
	socksCmdNotSupported = 0x07
	socksAddrNotSupp     = 0x08
)

// bufConn keeps bytes the client sent right after its request.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }

// CloseWrite forwards half-close when supported.
func (b *bufConn) CloseWrite() error {
	if x, ok := b.Conn.(interface{ CloseWrite() error }); ok {
		return x.CloseWrite()
	}
	return b.Conn.Close()
}

// Handshake performs the server side of a SOCKS5 (no auth, CONNECT) or
// SOCKS4/4a handshake on c. It returns the requested "host:port", a reply
// function that must be called with the dial result, and the connection to
// use afterwards (it may hold data the client sent early).
func Handshake(c net.Conn) (target string, reply func(error) error, conn net.Conn, err error) {
	br := bufio.NewReader(c)
	target, reply, err = handshake(c, br)
	return target, reply, &bufConn{Conn: c, r: br}, err
}

func handshake(c net.Conn, br *bufio.Reader) (target string, reply func(error) error, err error) {
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	defer func() {
		if err == nil {
			_ = c.SetDeadline(time.Time{})
		}
	}()
	ver, err := br.ReadByte()
	if err != nil {
		return "", nil, err
	}
	switch ver {
	case 5:
		return socks5(c, br)
	case 4:
		return socks4(c, br)
	}
	return "", nil, fmt.Errorf("unsupported SOCKS version %d", ver)
}

func socks5(c net.Conn, br *bufio.Reader) (string, func(error) error, error) {
	n, err := br.ReadByte()
	if err != nil {
		return "", nil, err
	}
	methods := make([]byte, n)
	if _, err := io.ReadFull(br, methods); err != nil {
		return "", nil, err
	}
	noAuth := false
	for _, m := range methods {
		if m == 0 {
			noAuth = true
		}
	}
	if !noAuth {
		_, _ = c.Write([]byte{5, 0xff})
		return "", nil, errors.New("socks5: client offers no acceptable auth method")
	}
	if _, err := c.Write([]byte{5, 0}); err != nil {
		return "", nil, err
	}
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(br, hdr); err != nil {
		return "", nil, err
	}
	if hdr[0] != 5 {
		return "", nil, errors.New("socks5: bad request version")
	}
	fail := func(code byte) { _, _ = c.Write([]byte{5, code, 0, 1, 0, 0, 0, 0, 0, 0}) }
	var host string
	switch hdr[3] {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(br, b); err != nil {
			return "", nil, err
		}
		host = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(br, b); err != nil {
			return "", nil, err
		}
		host = net.IP(b).String()
	case 3:
		l, err := br.ReadByte()
		if err != nil {
			return "", nil, err
		}
		b := make([]byte, l)
		if _, err := io.ReadFull(br, b); err != nil {
			return "", nil, err
		}
		host = string(b)
	default:
		fail(socksAddrNotSupp)
		return "", nil, errors.New("socks5: unsupported address type")
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(br, pb); err != nil {
		return "", nil, err
	}
	if hdr[1] != 1 {
		fail(socksCmdNotSupported)
		return "", nil, fmt.Errorf("socks5: command %d not supported", hdr[1])
	}
	target := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb))))
	return target, func(err error) error {
		if err != nil {
			fail(socksCode(err))
			return err
		}
		_, werr := c.Write([]byte{5, socksOK, 0, 1, 0, 0, 0, 0, 0, 0})
		return werr
	}, nil
}

func socksCode(err error) byte {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "refused"):
		return socksConnRefused
	case strings.Contains(s, "unreachable") && strings.Contains(s, "network"):
		return socksNetUnreachable
	case strings.Contains(s, "unreachable") || strings.Contains(s, "no such host") || strings.Contains(s, "timed out"):
		return socksHostUnreachable
	}
	return socksGeneralFailure
}

func readCString(br *bufio.Reader) (string, error) {
	s, err := br.ReadString(0)
	if err != nil {
		return "", err
	}
	if len(s) > 256 {
		return "", errors.New("socks4: string too long")
	}
	return strings.TrimSuffix(s, "\x00"), nil
}

func socks4(c net.Conn, br *bufio.Reader) (string, func(error) error, error) {
	hdr := make([]byte, 7) // cmd, port(2), ip(4)
	if _, err := io.ReadFull(br, hdr); err != nil {
		return "", nil, err
	}
	if _, err := readCString(br); err != nil { // user id
		return "", nil, err
	}
	reply := func(code byte) error {
		_, err := c.Write([]byte{0, code, 0, 0, 0, 0, 0, 0})
		return err
	}
	if hdr[0] != 1 {
		_ = reply(0x5b)
		return "", nil, errors.New("socks4: only CONNECT is supported")
	}
	port := binary.BigEndian.Uint16(hdr[1:3])
	ip := net.IP(hdr[3:7])
	host := ip.String()
	if ip[0] == 0 && ip[1] == 0 && ip[2] == 0 && ip[3] != 0 { // SOCKS4a
		d, err := readCString(br)
		if err != nil {
			return "", nil, err
		}
		host = d
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port))), func(err error) error {
		if err != nil {
			_ = reply(0x5b)
			return err
		}
		return reply(0x5a)
	}, nil
}
