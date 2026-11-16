// Package charset converts between UTF-8 and legacy encodings (Big5) for
// terminals and the editor. Conversion happens in Go so old devices' Big5
// output and config files are not garbled.
package charset

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// Supported lists encodings offered in the UI.
var Supported = []string{"UTF-8", "UTF-8 BOM", "Big5", "GBK", "Shift_JIS", "Windows-1252", "ISO-8859-1"}

// Lookup returns the encoding for a name; nil means UTF-8 (no conversion).
func Lookup(name string) (encoding.Encoding, error) {
	switch strings.ToLower(strings.ReplaceAll(name, "_", "-")) {
	case "", "utf-8", "utf8":
		return nil, nil
	case "utf-8 bom":
		return unicode.UTF8BOM, nil
	case "big5", "cp950":
		return traditionalchinese.Big5, nil
	case "gbk", "gb2312", "cp936":
		return simplifiedchinese.GBK, nil
	case "shift-jis", "sjis":
		return japanese.ShiftJIS, nil
	case "windows-1252", "cp1252":
		return charmap.Windows1252, nil
	case "iso-8859-1", "latin1":
		return charmap.ISO8859_1, nil
	}
	return nil, errors.New("unsupported encoding: " + name)
}

// StreamDecoder decodes a byte stream chunk by chunk, carrying incomplete
// multi-byte sequences over to the next chunk.
type StreamDecoder struct {
	t    transform.Transformer
	rest []byte
}

// NewStreamDecoder returns nil for UTF-8 (pass-through).
func NewStreamDecoder(name string) *StreamDecoder {
	enc, err := Lookup(name)
	if err != nil || enc == nil {
		return nil
	}
	return &StreamDecoder{t: enc.NewDecoder()}
}

// Decode converts src to UTF-8.
func (d *StreamDecoder) Decode(src []byte) []byte {
	if d == nil {
		return src
	}
	in := append(d.rest, src...)
	d.rest = nil
	out := make([]byte, 0, len(in)*2)
	buf := make([]byte, 4096)
	for len(in) > 0 {
		nDst, nSrc, err := d.t.Transform(buf, in, false)
		out = append(out, buf[:nDst]...)
		in = in[nSrc:]
		if errors.Is(err, transform.ErrShortSrc) {
			if len(in) > 8 { // not a plausible partial char; emit raw to avoid stalling
				out = append(out, in[0])
				in = in[1:]
				continue
			}
			d.rest = append([]byte(nil), in...)
			break
		}
		if errors.Is(err, transform.ErrShortDst) {
			continue
		}
		if err != nil {
			if len(in) > 0 {
				out = append(out, '?')
				in = in[1:]
			}
			d.t.Reset()
		}
	}
	return out
}

// Encode converts UTF-8 text to the named encoding (lossy chars become '?').
func Encode(name string, src []byte) []byte {
	enc, err := Lookup(name)
	if err != nil || enc == nil {
		return src
	}
	out, _, err := transform.Bytes(encoding.ReplaceUnsupported(enc.NewEncoder()), src)
	if err != nil {
		return src
	}
	return out
}

// DecodeAll converts a whole buffer to UTF-8.
func DecodeAll(name string, src []byte) (string, error) {
	enc, err := Lookup(name)
	if err != nil {
		return "", err
	}
	if enc == nil {
		return string(bytes.TrimPrefix(src, []byte("\xef\xbb\xbf"))), nil
	}
	out, _, err := transform.Bytes(enc.NewDecoder(), src)
	return string(out), err
}

// Detect guesses the encoding of file content: UTF-8 BOM, valid UTF-8, or
// Big5 when the bytes decode cleanly as Big5.
func Detect(b []byte) string {
	if bytes.HasPrefix(b, []byte("\xef\xbb\xbf")) {
		return "UTF-8 BOM"
	}
	if utf8.Valid(b) {
		return "UTF-8"
	}
	if out, _, err := transform.Bytes(traditionalchinese.Big5.NewDecoder(), b); err == nil && !bytes.ContainsRune(out, utf8.RuneError) {
		return "Big5"
	}
	return "Windows-1252"
}
