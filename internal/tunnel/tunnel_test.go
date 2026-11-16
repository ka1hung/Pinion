package tunnel

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ka1hung/pinion/internal/store"
)

// testSSHServer accepts any client and serves direct-tcpip channels.
func testSSHServer(t *testing.T) *ssh.Client {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer, _ := ssh.NewSignerFromKey(priv)
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					if nch.ChannelType() != "direct-tcpip" {
						nch.Reject(ssh.UnknownChannelType, "no")
						continue
					}
					var p struct {
						Host     string
						Port     uint32
						OrigHost string
						OrigPort uint32
					}
					if err := ssh.Unmarshal(nch.ExtraData(), &p); err != nil {
						nch.Reject(ssh.ConnectionFailed, "bad")
						continue
					}
					tc, err := net.Dial("tcp", net.JoinHostPort(p.Host, strconv.Itoa(int(p.Port))))
					if err != nil {
						nch.Reject(ssh.ConnectionFailed, err.Error())
						continue
					}
					ch, creqs, err := nch.Accept()
					if err != nil {
						tc.Close()
						continue
					}
					go ssh.DiscardRequests(creqs)
					go func() { io.Copy(ch, tc); ch.CloseWrite() }()
					go func() { io.Copy(tc, ch); tc.Close() }()
				}
			}()
		}
	}()
	c, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{User: "x", HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// echoServer echoes everything back.
func echoServer(t *testing.T) (string, int) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port
}

func roundTrip(t *testing.T, c net.Conn, msg string) {
	t.Helper()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != msg {
		t.Fatalf("got %q", buf)
	}
}

func TestLocalForward(t *testing.T) {
	client := testSSHServer(t)
	h, p := echoServer(t)
	f, err := Start(client, store.Tunnel{Kind: "L", BindHost: "127.0.0.1", BindPort: 0, TargetHost: h, TargetPort: p})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Stop()
	c, err := net.Dial("tcp", f.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	roundTrip(t, c, "hello tunnel")
	var s Stats
	for i := 0; i < 100; i++ {
		if s = f.Stats(); s.BytesOut == 12 && s.BytesIn == 12 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s.BytesOut != 12 || s.BytesIn != 12 || s.Conns < 1 {
		t.Fatalf("stats %+v", s)
	}
	c.Close()
	f.Stop()
	select {
	case <-f.Done():
	case <-time.After(time.Second):
		t.Fatal("not stopped")
	}
	if f.Err() != nil {
		t.Fatal(f.Err())
	}
}

func TestDynamicForwardSOCKS5AndSOCKS4a(t *testing.T) {
	client := testSSHServer(t)
	_, p := echoServer(t)
	f, err := Start(client, store.Tunnel{Kind: "D", BindPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer f.Stop()

	// SOCKS5 with domain name
	c, err := net.Dial("tcp", f.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte{5, 1, 0})
	r := make([]byte, 2)
	io.ReadFull(c, r)
	if !bytes.Equal(r, []byte{5, 0}) {
		t.Fatalf("greeting %v", r)
	}
	host := "localhost"
	req := []byte{5, 1, 0, 3, byte(len(host))}
	req = append(req, host...)
	req = binary.BigEndian.AppendUint16(req, uint16(p))
	c.Write(req)
	rep := make([]byte, 10)
	io.ReadFull(c, rep)
	if rep[1] != 0 {
		t.Fatalf("reply %v", rep)
	}
	roundTrip(t, c, "via socks5")
	c.Close()

	// SOCKS4a
	c, _ = net.Dial("tcp", f.Addr().String())
	c.SetDeadline(time.Now().Add(5 * time.Second))
	req = []byte{4, 1}
	req = binary.BigEndian.AppendUint16(req, uint16(p))
	req = append(req, 0, 0, 0, 1, 'u', 0)
	req = append(req, "127.0.0.1"...)
	req = append(req, 0)
	c.Write(req)
	rep = make([]byte, 8)
	io.ReadFull(c, rep)
	if rep[1] != 0x5a {
		t.Fatalf("socks4 reply %v", rep)
	}
	roundTrip(t, c, "via socks4a")
	c.Close()

	// SOCKS5 to a closed port → failure reply
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	dead := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	c, _ = net.Dial("tcp", f.Addr().String())
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write([]byte{5, 1, 0})
	io.ReadFull(c, r)
	req = []byte{5, 1, 0, 1, 127, 0, 0, 1}
	req = binary.BigEndian.AppendUint16(req, uint16(dead))
	c.Write(req)
	rep = make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil {
		t.Fatal(err)
	}
	if rep[1] == 0 {
		t.Fatalf("expected failure reply, got %v", rep)
	}
	c.Close()
}

func TestSOCKS5Parser(t *testing.T) {
	cases := []struct {
		name    string
		in      []byte
		target  string
		wantErr bool
		reply   []byte
	}{
		{"ipv4", []byte{5, 1, 0, 5, 1, 0, 1, 10, 0, 0, 1, 0, 80}, "10.0.0.1:80", false, []byte{5, 0}},
		{"ipv6", append(append([]byte{5, 1, 0, 5, 1, 0, 4}, net.ParseIP("::1").To16()...), 1, 187), "[::1]:443", false, []byte{5, 0}},
		{"domain", append(append([]byte{5, 2, 2, 0, 5, 1, 0, 3, 11}, "example.com"...), 0, 22), "example.com:22", false, []byte{5, 0}},
		{"noauth missing", []byte{5, 1, 2}, "", true, []byte{5, 0xff}},
		{"bind cmd", []byte{5, 1, 0, 5, 2, 0, 1, 1, 2, 3, 4, 0, 80}, "", true, []byte{5, 0, 5, socksCmdNotSupported}},
		{"bad version", []byte{9}, "", true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := net.Pipe()
			defer a.Close()
			go func() { b.Write(tc.in) }()
			out := make(chan []byte, 1)
			go func() {
				var buf bytes.Buffer
				tmp := make([]byte, 64)
				b.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
				for {
					n, err := b.Read(tmp)
					buf.Write(tmp[:n])
					if err != nil {
						break
					}
				}
				out <- buf.Bytes()
			}()
			target, _, _, err := Handshake(a)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
			if target != tc.target {
				t.Fatalf("target %q", target)
			}
			got := <-out
			if !bytes.HasPrefix(got, tc.reply) {
				t.Fatalf("reply %v want prefix %v", got, tc.reply)
			}
			b.Close()
		})
	}
}

func TestValidateAndBindError(t *testing.T) {
	if Validate(store.Tunnel{Kind: "L", BindPort: 80}) == nil {
		t.Fatal("L without target should fail")
	}
	if Validate(store.Tunnel{Kind: "X"}) == nil {
		t.Fatal("bad kind")
	}
	if Validate(store.Tunnel{Kind: "D", BindPort: 1080}) != nil {
		t.Fatal("D ok")
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	_, err := net.Listen("tcp", ln.Addr().String())
	if err == nil {
		t.Skip("platform allows double bind")
	}
	e := FriendlyBindError(ln.Addr().String(), err)
	if !bytes.Contains([]byte(e.Error()), []byte("already in use")) {
		t.Fatal(e)
	}
}
