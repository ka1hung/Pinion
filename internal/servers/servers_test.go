package servers

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pin/tftp/v3"
)

func freePort(t *testing.T, network string) int {
	t.Helper()
	if network == "udp" {
		c, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		return c.LocalAddr().(*net.UDPAddr).Port
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type logSink struct {
	mu sync.Mutex
	es []LogEntry
}

func (l *logSink) add(e LogEntry) { l.mu.Lock(); l.es = append(l.es, e); l.mu.Unlock() }
func (l *logSink) has(action, file string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.es {
		if e.Action == action && strings.Contains(e.File, file) {
			return true
		}
	}
	return false
}

func newTestManager(t *testing.T) (*Manager, *logSink, string) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "fw.bin"), bytes.Repeat([]byte("firmware"), 5000), 0o644)
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.WriteFile(filepath.Join(root, "sub", "a.txt"), []byte("hello"), 0o644)
	m := NewManager()
	sink := &logSink{}
	m.OnLog = sink.add
	t.Cleanup(m.StopAll)
	return m, sink, root
}

func TestTFTPReadOnly(t *testing.T) {
	m, sink, root := newTestManager(t)
	port := freePort(t, "udp")
	if err := m.Start(ServerConfig{Kind: KindTFTP, Root: root, BindIP: "127.0.0.1", Port: port}); err != nil {
		t.Fatal(err)
	}
	c, err := tftp.NewClient("127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	c.SetTimeout(2 * time.Second)
	c.SetBlockSize(1468)
	wt, err := c.Receive("fw.bin", "octet")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := wt.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 40000 {
		t.Fatalf("got %d bytes", buf.Len())
	}
	// traversal
	if wt, err := c.Receive("../../etc/passwd", "octet"); err == nil {
		if _, err := wt.WriteTo(io.Discard); err == nil {
			t.Fatal("traversal should fail")
		}
	}
	// write rejected when read-only
	rf, err := c.Send("up.bin", "octet")
	if err == nil {
		if _, err := rf.ReadFrom(strings.NewReader("x")); err == nil {
			t.Fatal("write should fail on read-only server")
		}
	}
	if _, err := os.Stat(filepath.Join(root, "up.bin")); err == nil {
		t.Fatal("file written on read-only server")
	}
	time.Sleep(50 * time.Millisecond)
	if !sink.has("read", "fw.bin") {
		t.Fatalf("no read log: %+v", sink.es)
	}
	st := m.Status()
	if !st[0].Running || st[0].Kind != KindTFTP || !strings.HasPrefix(st[0].URL, "tftp://127.0.0.1:") {
		t.Fatalf("status %+v", st[0])
	}
	if err := m.Stop(KindTFTP); err != nil {
		t.Fatal(err)
	}
	if m.Status()[0].Running {
		t.Fatal("still running")
	}
}

func TestTFTPWritable(t *testing.T) {
	m, _, root := newTestManager(t)
	port := freePort(t, "udp")
	if err := m.Start(ServerConfig{Kind: KindTFTP, Root: root, BindIP: "127.0.0.1", Port: port, Writable: true}); err != nil {
		t.Fatal(err)
	}
	c, _ := tftp.NewClient("127.0.0.1:" + strconv.Itoa(port))
	c.SetTimeout(2 * time.Second)
	rf, err := c.Send("cfg/up.txt", "octet")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rf.ReadFrom(strings.NewReader("uploaded")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	b, err := os.ReadFile(filepath.Join(root, "cfg", "up.txt"))
	if err != nil || string(b) != "uploaded" {
		t.Fatalf("%q %v", b, err)
	}
}

func TestHTTPHandler(t *testing.T) {
	m, sink, root := newTestManager(t)
	h := NewHTTPHandler(m, ServerConfig{Kind: KindHTTP, Root: root})
	srv := httptest.NewServer(h)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(body), "fw.bin") || !strings.Contains(string(body), "sub/") || strings.Contains(string(body), "Upload") {
		t.Fatalf("listing: %s", body)
	}
	res, _ = http.Get(srv.URL + "/sub/a.txt")
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "hello" {
		t.Fatalf("get %q", body)
	}
	// Range request
	req, _ := http.NewRequest("GET", srv.URL+"/sub/a.txt", nil)
	req.Header.Set("Range", "bytes=1-2")
	res, _ = http.DefaultClient.Do(req)
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 206 || string(body) != "el" {
		t.Fatalf("range %d %q", res.StatusCode, body)
	}
	// traversal via raw path
	conn, _ := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	conn.Write([]byte("GET /../../../etc/passwd HTTP/1.0\r\n\r\n"))
	line, _ := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if !strings.Contains(line, "403") && !strings.Contains(line, "404") && !strings.Contains(line, "400") {
		t.Fatalf("traversal status %q", line)
	}
	if _, err := resolvePath(root, "/a/../../x"); err != nil {
		t.Fatal("cleaned path should resolve inside root")
	}
	// PUT rejected when read-only
	req, _ = http.NewRequest("PUT", srv.URL+"/new.txt", strings.NewReader("x"))
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("put status %d", res.StatusCode)
	}
	if !sink.has("read", "a.txt") || !sink.has("list", "/") {
		t.Fatalf("logs %+v", sink.es)
	}
}

func TestHTTPWritableAndAuth(t *testing.T) {
	m, _, root := newTestManager(t)
	h := NewHTTPHandler(m, ServerConfig{Kind: KindHTTP, Root: root, Writable: true, User: "u", Password: "p"})
	srv := httptest.NewServer(h)
	defer srv.Close()
	res, _ := http.Get(srv.URL + "/")
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("want 401, got %d", res.StatusCode)
	}
	req, _ := http.NewRequest("PUT", srv.URL+"/dir/new.txt", strings.NewReader("data"))
	req.SetBasicAuth("u", "p")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 201 {
		t.Fatalf("put %d", res.StatusCode)
	}
	b, _ := os.ReadFile(filepath.Join(root, "dir", "new.txt"))
	if string(b) != "data" {
		t.Fatalf("%q", b)
	}
}

func TestHTTPAndFTPStart(t *testing.T) {
	m, sink, root := newTestManager(t)
	hp := freePort(t, "tcp")
	if err := m.Start(ServerConfig{Kind: KindHTTP, Root: root, BindIP: "127.0.0.1", Port: hp}); err != nil {
		t.Fatal(err)
	}
	res, err := http.Get("http://127.0.0.1:" + strconv.Itoa(hp) + "/sub/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	fp := freePort(t, "tcp")
	if err := m.Start(ServerConfig{Kind: KindFTP, Root: root, BindIP: "127.0.0.1", Port: fp, PassivePorts: "50000-50010"}); err != nil {
		t.Fatal(err)
	}
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(fp), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(3 * time.Second))
	r := bufio.NewReader(c)
	line, _ := r.ReadString('\n')
	if !strings.HasPrefix(line, "220") {
		t.Fatalf("banner %q", line)
	}
	c.Write([]byte("USER anonymous\r\n"))
	line, _ = r.ReadString('\n')
	if !strings.HasPrefix(line, "331") {
		t.Fatalf("user %q", line)
	}
	c.Write([]byte("PASS x\r\n"))
	line, _ = r.ReadString('\n')
	if !strings.HasPrefix(line, "230") {
		t.Fatalf("pass %q", line)
	}
	c.Close()
	if !sink.has("login", "") {
		t.Fatal("no login log")
	}
	// port in use → friendly error
	err = m.Start(ServerConfig{Kind: KindTFTP, Root: root, BindIP: "127.0.0.1", Port: freePort(t, "udp")})
	if err != nil {
		t.Fatal(err)
	}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port
	err = m.Start(ServerConfig{Kind: KindHTTP, Root: root, BindIP: "127.0.0.1", Port: busy})
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("want in-use error, got %v", err)
	}
	if st := m.Status(); st[1].Running || st[1].Error == "" {
		t.Fatalf("http status %+v", st[1])
	}
	m.StopAll()
	for _, s := range m.Status() {
		if s.Running {
			t.Fatalf("%s still running", s.Kind)
		}
	}
	if _, err := ParsePortRange("10-5"); err == nil {
		t.Fatal("bad range accepted")
	}
}

func TestIdleStop(t *testing.T) {
	m, _, root := newTestManager(t)
	changed := 0
	m.OnChange = func() { changed++ }
	if err := m.Start(ServerConfig{Kind: KindHTTP, Root: root, BindIP: "127.0.0.1", Port: freePort(t, "tcp"), IdleMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	m.checkIdle(time.Now().Add(30 * time.Second))
	if !m.Status()[1].Running {
		t.Fatal("stopped too early")
	}
	m.checkIdle(time.Now().Add(2 * time.Minute))
	if st := m.Status()[1]; st.Running || st.Error == "" {
		t.Fatalf("not idle-stopped: %+v", st)
	}
}

func TestInterfaces(t *testing.T) {
	ifs, err := Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(ifs) < 2 || ifs[len(ifs)-1].IP != "127.0.0.1" || ifs[len(ifs)-2].IP != "0.0.0.0" {
		t.Fatalf("%+v", ifs)
	}
}
