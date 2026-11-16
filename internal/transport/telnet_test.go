package transport

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestTelnetParseSplitAndNegotiate(t *testing.T) {
	srv, cli := net.Pipe()
	defer srv.Close()
	tn := &Telnet{conn: cli, Cols: 100, Rows: 40, remote: map[byte]bool{}, local: map[byte]bool{}, refused: map[[2]byte]bool{}}
	stream := []byte{'h', 'i', tnIAC, tnWILL, optEcho, 'a', '\r', 0, 'b', tnIAC, tnIAC, tnIAC, tnDO, optNAWS, tnIAC, tnDO, 99, tnIAC, tnSB, optTType, ttypeSEND, tnIAC, tnSE, 'z'}
	replies := make(chan []byte, 1)
	go func() {
		var all []byte
		buf := make([]byte, 256)
		srv.SetReadDeadline(time.Now().Add(time.Second))
		for {
			n, err := srv.Read(buf)
			all = append(all, buf[:n]...)
			if err != nil {
				break
			}
		}
		replies <- all
	}()
	var out []byte
	for i := range stream { // feed byte by byte to exercise split sequences
		out = tn.parse(stream[i:i+1], out)
	}
	if string(out) != "hia\rb\xffz" {
		t.Fatalf("data %q", out)
	}
	tn.Resize(300, 255)
	time.Sleep(50 * time.Millisecond)
	srv.SetReadDeadline(time.Now())
	got := <-replies
	want := [][]byte{
		{tnIAC, tnDO, optEcho},
		{tnIAC, tnWILL, optNAWS},
		{tnIAC, tnSB, optNAWS, 0, 100, 0, 40, tnIAC, tnSE},
		{tnIAC, tnWONT, 99},
		append(append([]byte{tnIAC, tnSB, optTType, ttypeIS}, "XTERM-256COLOR"...), tnIAC, tnSE),
		{tnIAC, tnSB, optNAWS, 1, 44, 0, tnIAC, tnIAC, tnIAC, tnSE},
	}
	if !bytes.Equal(got, bytes.Join(want, nil)) {
		t.Fatalf("replies %v", got)
	}
}

func TestTelnetNoLoop(t *testing.T) {
	srv, cli := net.Pipe()
	defer srv.Close()
	tn := &Telnet{conn: cli, remote: map[byte]bool{}, local: map[byte]bool{}, refused: map[[2]byte]bool{}}
	go io.Copy(io.Discard, srv)
	n := 0
	tn.wmu.Lock()
	tn.wmu.Unlock()
	// repeated DO for unsupported option must be refused only once
	for range 3 {
		tn.negotiate(tnDO, 77)
	}
	if len(tn.refused) != 1 {
		t.Fatal(n, tn.refused)
	}
}

func TestEncodeOut(t *testing.T) {
	if got := encodeOut([]byte("a\r\xffb\r\n"), false); string(got) != "a\r\n\xff\xffb\r\n" {
		t.Fatalf("%q", got)
	}
}

func TestTelnetLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		c.Write([]byte{tnIAC, tnWILL, optSGA})
		c.Write([]byte("login: "))
		var all []byte
		buf := make([]byte, 64)
		for !bytes.Contains(all, []byte("x")) {
			n, err := c.Read(buf)
			if err != nil {
				break
			}
			all = append(all, buf[:n]...)
		}
		if !bytes.HasPrefix(all, []byte{tnIAC, tnDO, optSGA}) {
			c.Write([]byte("bad negotiation"))
		}
		c.Write([]byte("x"))
		c.Close()
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	tn := &Telnet{Host: "127.0.0.1", Port: port}
	if err := tn.Open(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer tn.Close()
	var got []byte
	buf := make([]byte, 64)
	for !bytes.Contains(got, []byte("login: ")) {
		n, err := tn.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, buf[:n]...)
	}
	if string(got) != "login: " {
		t.Fatalf("%q", got)
	}
	tn.Write([]byte("x"))
	// server checks our DO SGA reply then echoes "x"
	got = got[:0]
	for {
		n, err := tn.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			break
		}
	}
	if string(got) != "x" {
		t.Fatalf("echo %q", got)
	}
	_ = strconv.Itoa
}
