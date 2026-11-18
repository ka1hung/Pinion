package privbind

import (
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func pair(t *testing.T) (parent, child *os.File) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	return os.NewFile(uintptr(fds[0]), "p"), os.NewFile(uintptr(fds[1]), "c")
}

func allowAll(string, int) bool { return true }

// The fd-passing exchange, in process, on an unprivileged port.
func TestExchangeTCP(t *testing.T) {
	parent, child := pair(t)
	defer parent.Close()
	done := make(chan error, 1)
	go func() { done <- serve(child, allowAll); child.Close() }()

	f, err := exchange(parent, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ln, err := net.FileListener(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Write([]byte("hi"))
			c.Close()
		}
	}()
	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	buf := make([]byte, 2)
	if _, err := c.Read(buf); err != nil || string(buf) != "hi" {
		t.Fatalf("read %q %v", buf, err)
	}
}

func TestExchangeUDPAllInterfaces(t *testing.T) {
	parent, child := pair(t)
	defer parent.Close()
	go func() { serve(child, allowAll); child.Close() }()

	f, err := exchange(parent, "udp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	pc, err := net.FilePacketConn(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	port := pc.LocalAddr().(*net.UDPAddr).Port
	c, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("x"))
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 4)
	if n, _, err := pc.ReadFrom(buf); err != nil || string(buf[:n]) != "x" {
		t.Fatalf("got %q %v", buf[:n], err)
	}
}

func TestExchangeRefused(t *testing.T) {
	parent, child := pair(t)
	defer parent.Close()
	go func() { serve(child, Allowed); child.Close() }()

	_, err := exchange(parent, "tcp", "127.0.0.1:22")
	if err == nil || !strings.Contains(err.Error(), "not one Pinion may open") {
		t.Fatalf("want refusal, got %v", err)
	}
}

func TestAllowed(t *testing.T) {
	for _, c := range []struct {
		proto string
		port  int
		want  bool
	}{{"udp", 69, true}, {"tcp", 69, false}, {"tcp", 21, true}, {"tcp", 80, true}, {"tcp", 443, true}, {"tcp", 22, false}, {"udp", 53, false}} {
		if Allowed(c.proto, c.port) != c.want {
			t.Errorf("Allowed(%s,%d) != %v", c.proto, c.port, c.want)
		}
	}
}
