package termlog

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestPathAndStrip(t *testing.T) {
	p := Path(Options{Dir: "/logs", Folder: "Site A/Rack:1", Name: "sw/01"}, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if !strings.HasSuffix(strings.ReplaceAll(p, `\`, "/"), "/logs/Site A/Rack_1/sw_01_20260102-030405.log") {
		t.Fatal(p)
	}
	got := string(StripANSI([]byte("\x1b[1;32mok\x1b[0m\r\n\x1b]0;title\x07x")))
	if got != "ok\nx" {
		t.Fatalf("%q", got)
	}
}

func TestLoggerTimestamps(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(Options{Dir: dir, Name: "a", StripANSI: true, Timestamps: true})
	if err != nil {
		t.Fatal(err)
	}
	l.Write([]byte("line1\r\nli"))
	l.Write([]byte("ne2\n"))
	l.Close()
	b, _ := os.ReadFile(l.FilePath())
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[1], "] line2") || !strings.HasPrefix(lines[0], "[") {
		t.Fatalf("%q", string(b))
	}
}
