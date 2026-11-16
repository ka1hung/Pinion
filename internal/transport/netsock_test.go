package transport

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/ka1hung/pinion/internal/store"
)

func readN(t *testing.T, r io.Reader, want string) {
	t.Helper()
	var got []byte
	buf := make([]byte, 64)
	deadline := time.After(2 * time.Second)
	for len(got) < len(want) {
		ch := make(chan []byte, 1)
		go func() {
			n, _ := r.Read(buf)
			ch <- append([]byte(nil), buf[:n]...)
		}()
		select {
		case b := <-ch:
			got = append(got, b...)
		case <-deadline:
			t.Fatalf("timeout, got %q", got)
		}
	}
	if string(got) != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func waitNotice(t *testing.T, ch <-chan string, sub string) {
	t.Helper()
	for {
		select {
		case n := <-ch:
			if strings.Contains(n, sub) {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no notice containing %q", sub)
		}
	}
}

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestTCPServerAndClient(t *testing.T) {
	port := freePort(t)
	srv := &NetSock{Proto: "tcp", Config: &store.NetConfig{Mode: "server", Host: "127.0.0.1", Port: port}}
	notes := srv.Notices()
	if err := srv.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	waitNotice(t, notes, "Listening on TCP 127.0.0.1:")
	if _, err := srv.Write([]byte("x")); err == nil || !strings.Contains(err.Error(), "no client") {
		t.Fatal("expected no client error", err)
	}
	cli := &NetSock{Proto: "tcp", Config: &store.NetConfig{Mode: "client", Host: "127.0.0.1", Port: port}}
	if err := cli.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	waitNotice(t, notes, "connected (1 connected)")
	if !strings.Contains(srv.Describe(), "(1 client)") {
		t.Fatal(srv.Describe())
	}
	cli.Write([]byte(`{"a":1}`))
	readN(t, srv, `{"a":1}`)
	srv.Write([]byte("pong"))
	readN(t, cli, "pong")
	cli.Close()
	waitNotice(t, notes, "disconnected (0 connected)")
}

func TestUDPServerAndClient(t *testing.T) {
	pc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()
	srv := &NetSock{Proto: "udp", Config: &store.NetConfig{Mode: "server", Host: "127.0.0.1", Port: port}}
	notes := srv.Notices()
	if err := srv.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if _, err := srv.Write([]byte("x")); err == nil {
		t.Fatal("expected no peer error")
	}
	cli := &NetSock{Proto: "udp", Config: &store.NetConfig{Mode: "client", Host: "127.0.0.1", Port: port}}
	if err := cli.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	cli.Write([]byte("hello"))
	readN(t, srv, "hello")
	waitNotice(t, notes, "UDP peer 127.0.0.1:")
	srv.Write([]byte("world"))
	readN(t, cli, "world")
	if !strings.Contains(srv.Describe(), "(1 peer)") {
		t.Fatal(srv.Describe())
	}
}

func TestNetSockCloseUnblocksRead(t *testing.T) {
	port := freePort(t)
	srv := &NetSock{Proto: "tcp", Config: &store.NetConfig{Mode: "server", Host: "127.0.0.1", Port: port}}
	if err := srv.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := srv.Read(make([]byte, 8))
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	srv.Close()
	select {
	case err := <-done:
		if err != io.EOF {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("read not unblocked")
	}
}
