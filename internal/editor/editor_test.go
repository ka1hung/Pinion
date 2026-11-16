package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func TestBig5CRLFRoundTrip(t *testing.T) {
	raw := Encode("[網路]\nip=192.168.1.1\n名稱=控制器\n", "Big5", CRLF)
	text, enc, eol, err := Decode(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	if enc != "Big5" || eol != CRLF {
		t.Fatalf("enc=%s eol=%s", enc, eol)
	}
	if text != "[網路]\nip=192.168.1.1\n名稱=控制器\n" {
		t.Fatalf("text=%q", text)
	}
	if got := Encode(text, enc, eol); string(got) != string(raw) {
		t.Fatalf("round trip mismatch")
	}
}

func TestUTF8BOMKept(t *testing.T) {
	raw := []byte("\xef\xbb\xbfa\nb\n")
	text, enc, eol, _ := Decode(raw, "auto")
	if text != "a\nb\n" || enc != "UTF-8 BOM" || eol != LF {
		t.Fatalf("%q %s %s", text, enc, eol)
	}
	if string(Encode(text, enc, eol)) != string(raw) {
		t.Fatal("BOM lost")
	}
}

func localResolver(source, tabID string) (FS, error) { return LocalFS{}, nil }

func TestConflictDetectionLocal(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.conf")
	os.WriteFile(p, []byte("one\r\n"), 0o640)
	m := NewManager(localResolver, func() int64 { return 1 << 20 }, nil)
	d, err := m.Open(SourceLocal, "", p, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if d.Content != "one\n" || d.EOL != CRLF {
		t.Fatalf("%+v", d)
	}
	r, err := m.Save(SaveRequest{DocID: d.ID, Content: "two\n"})
	if err != nil || !r.OK {
		t.Fatalf("%v %+v", err, r)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "two\r\n" {
		t.Fatalf("%q", b)
	}
	if st, _ := os.Stat(p); runtime.GOOS != "windows" && st.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", st.Mode())
	}
	// someone else changes the file
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(p, []byte("external change\r\n"), 0o640)
	r, err = m.Save(SaveRequest{DocID: d.ID, Content: "three\n"})
	if err != nil || r.OK || !r.Conflict || r.RemoteContent != "external change\n" {
		t.Fatalf("%v %+v", err, r)
	}
	r, err = m.Save(SaveRequest{DocID: d.ID, Content: "three\n", Force: true})
	if err != nil || !r.OK {
		t.Fatalf("%v %+v", err, r)
	}
	// after a successful save no conflict
	r, _ = m.Save(SaveRequest{DocID: d.ID, Content: "four\n"})
	if !r.OK {
		t.Fatalf("%+v", r)
	}
}

func TestBigFileGuard(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.log")
	os.WriteFile(p, make([]byte, 2048), 0o600)
	m := NewManager(localResolver, func() int64 { return 1024 }, nil)
	d, _ := m.Open(SourceLocal, "", p, "", false)
	if !d.TooLarge || d.Content != "" {
		t.Fatalf("%+v", d)
	}
	d, _ = m.Open(SourceLocal, "", p, "", true)
	if d.TooLarge || !d.ReadOnly || len(d.Content) != 2048 {
		t.Fatalf("readonly=%v len=%d", d.ReadOnly, len(d.Content))
	}
	r, _ := m.Save(SaveRequest{DocID: d.ID, Content: "x"})
	if r.OK {
		t.Fatal("read-only doc saved")
	}
}

func TestShellQuote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	for _, s := range []string{"", "plain", "it's", `a "b" $HOME`, "x;rm -rf /", "新 檔案"} {
		out, err := exec.Command("sh", "-c", "printf %s "+ShellQuote(s)).Output()
		if err != nil || string(out) != s {
			t.Fatalf("%q -> %q (%v)", s, out, err)
		}
	}
}

func TestBuildCommand(t *testing.T) {
	cases := []struct {
		tmpl string
		want []string
	}{
		{"code --wait {file}", []string{"code", "--wait", "/tmp/a b.txt"}},
		{"code", []string{"code", "/tmp/a b.txt"}},
		{`"C:\Program Files\Notepad++\notepad++.exe" -multiInst "{file}"`, []string{`C:\Program Files\Notepad++\notepad++.exe`, "-multiInst", "/tmp/a b.txt"}},
		{`subl -n '--x={file}'`, []string{"subl", "-n", "--x=/tmp/a b.txt"}},
	}
	for _, c := range cases {
		got, err := BuildCommand(c.tmpl, "/tmp/a b.txt")
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s: %q %v", c.tmpl, got, err)
		}
	}
	if _, err := BuildCommand(`code "x`, "f"); err == nil {
		t.Fatal("expected unterminated quote error")
	}
	if _, err := BuildCommand("  ", "f"); err == nil {
		t.Fatal("expected empty command error")
	}
}

func TestExternalSyncAndConflict(t *testing.T) {
	remoteDir := t.TempDir()
	remote := filepath.Join(remoteDir, "nginx.conf")
	os.WriteFile(remote, []byte("v1\n"), 0o644)
	states := make(chan ExternalState, 32)
	x := NewExternal(localResolver, filepath.Join(t.TempDir(), "pinion-edit"), func(s ExternalState) { states <- s })
	x.debounce = 50 * time.Millisecond
	var local string
	id, err := x.Open("t1", "host:22", remote, func(l string) error { local = l; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(local) != "nginx.conf" {
		t.Fatal(local)
	}
	wait := func(want string) ExternalState {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case s := <-states:
				if s.State == want {
					return s
				}
			case <-deadline:
				t.Fatalf("timeout waiting for %s", want)
			}
		}
	}
	wait(ExtWatching)
	os.WriteFile(local, []byte("v2\n"), 0o600)
	wait(ExtSynced)
	if b, _ := os.ReadFile(remote); string(b) != "v2\n" {
		t.Fatalf("remote=%q", b)
	}
	// remote changed behind our back → conflict, not overwritten
	time.Sleep(20 * time.Millisecond)
	os.WriteFile(remote, []byte("server edit\n"), 0o644)
	os.WriteFile(local, []byte("v3\n"), 0o600)
	wait(ExtConflict)
	if b, _ := os.ReadFile(remote); string(b) != "server edit\n" {
		t.Fatalf("remote overwritten: %q", b)
	}
	if err := x.ForceUpload(id); err != nil {
		t.Fatal(err)
	}
	wait(ExtSynced)
	if b, _ := os.ReadFile(remote); string(b) != "v3\n" {
		t.Fatalf("remote=%q", b)
	}
	x.StopTab("t1")
	wait(ExtStopped)
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Fatal("temp copy not removed")
	}
}
