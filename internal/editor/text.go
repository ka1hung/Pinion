// Package editor implements the built-in editor backend (encoding / EOL
// conversion, conflict detection, sudo save) and the external-editor sync.
package editor

import (
	"bytes"
	"strings"

	"github.com/ka1hung/pinion/internal/charset"
)

// EOL values.
const (
	LF   = "LF"
	CRLF = "CRLF"
)

// DetectEOL returns CRLF when most line breaks are CRLF.
func DetectEOL(b []byte) string {
	crlf := bytes.Count(b, []byte("\r\n"))
	lf := bytes.Count(b, []byte("\n")) - crlf
	if crlf > 0 && crlf >= lf {
		return CRLF
	}
	return LF
}

// Decode converts raw file bytes to UTF-8 text with LF line endings.
// encoding "" or "auto" means detect. Returns text, encoding, eol.
func Decode(raw []byte, encoding string) (string, string, string, error) {
	if encoding == "" || strings.EqualFold(encoding, "auto") {
		encoding = charset.Detect(raw)
	}
	text, err := charset.DecodeAll(encoding, raw)
	if err != nil {
		return "", encoding, LF, err
	}
	text = strings.TrimPrefix(text, "\ufeff")
	eol := DetectEOL([]byte(text))
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return text, encoding, eol, nil
}

// Encode converts UTF-8/LF text to file bytes in the given encoding and EOL.
func Encode(text, encoding, eol string) []byte {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if eol == CRLF {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return charset.Encode(encoding, []byte(text))
}
