package charset

import (
	"testing"
)

func TestBig5RoundTripAndStreaming(t *testing.T) {
	src := "設定檔 config=開啟\r\n"
	b := Encode("Big5", []byte(src))
	if string(b) == src {
		t.Fatal("expected Big5 bytes to differ from UTF-8")
	}
	if Detect(b) != "Big5" {
		t.Fatalf("detect = %s", Detect(b))
	}
	// split in the middle of every possible position
	for i := 1; i < len(b); i++ {
		d := NewStreamDecoder("Big5")
		got := string(d.Decode(b[:i])) + string(d.Decode(b[i:]))
		if got != src {
			t.Fatalf("split %d: got %q", i, got)
		}
	}
	s, err := DecodeAll("Big5", b)
	if err != nil || s != src {
		t.Fatalf("DecodeAll = %q %v", s, err)
	}
}

func TestDetectUTF8(t *testing.T) {
	if Detect([]byte("中文 utf8")) != "UTF-8" {
		t.Fatal("utf8")
	}
	if Detect([]byte("\xef\xbb\xbfabc")) != "UTF-8 BOM" {
		t.Fatal("bom")
	}
	if NewStreamDecoder("UTF-8") != nil {
		t.Fatal("utf8 decoder should be nil")
	}
}
