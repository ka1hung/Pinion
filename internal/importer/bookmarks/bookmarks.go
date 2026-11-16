// Package bookmarks imports sessions from a terminal bookmark file (.ini or an
// exported .mxtsessions file. Only SSH, Telnet, Serial and local Shell
// sessions are converted; everything else is reported as skipped.
// Passwords are never imported (they are stored encrypted in the source file).
package bookmarks

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"

	"github.com/ka1hung/pinion/internal/store"
)

// Skipped is a session that was not imported.
type Skipped struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

// Result is the outcome of parsing a bookmark file.
type Result struct {
	Encoding string           `json:"encoding"`
	Sessions []*store.Session `json:"sessions"`
	Folders  []string         `json:"folders"`
	Skipped  []Skipped        `json:"skipped"`
	Warnings []string         `json:"warnings"`
}

// Session type codes (second '#' field).
const (
	typeSSH     = 0
	typeTelnet  = 1
	typeRsh     = 2
	typeXdmcp   = 3
	typeRDP     = 4
	typeVNC     = 5
	typeFTP     = 6
	typeSFTP    = 7
	typeSerial  = 8
	typeFile    = 9
	typeShell   = 10
	typeBrowser = 11
	typeMosh    = 12
	typeS3      = 13
	typeWSL     = 14
)

var typeNames = map[int]string{
	typeSSH: "SSH", typeTelnet: "Telnet", typeRsh: "Rsh", typeXdmcp: "XDMCP", typeRDP: "RDP",
	typeVNC: "VNC", typeFTP: "FTP", typeSFTP: "SFTP", typeSerial: "Serial", typeFile: "File",
	typeShell: "Shell", typeBrowser: "Browser", typeMosh: "Mosh", typeS3: "AWS S3", typeWSL: "WSL",
}

func typeName(t int) string {
	if n, ok := typeNames[t]; ok {
		return n
	}
	return "type " + strconv.Itoa(t)
}

func skipReason(t int) string {
	switch t {
	case typeRDP, typeVNC:
		return typeName(t) + " sessions are not supported yet (planned: launch the system client)"
	case typeSFTP, typeFTP:
		return typeName(t) + "-only sessions are not supported; use an SSH session with the SFTP panel"
	case typeMosh:
		return "Mosh is not supported; import it as SSH manually"
	case typeWSL:
		return "WSL sessions are not supported; use a local shell running wsl.exe"
	default:
		return typeName(t) + " sessions are not supported"
	}
}

// Decode converts file bytes to UTF-8 and reports the detected encoding:
// UTF-8 (with or without BOM), Big5 (cp950) or Windows-1252.
func Decode(data []byte) (string, string) {
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		return string(data[3:]), "UTF-8 BOM"
	}
	if utf8.Valid(data) {
		return string(data), "UTF-8"
	}
	if out, _, err := transform.Bytes(traditionalchinese.Big5.NewDecoder(), data); err == nil && !bytes.ContainsRune(out, utf8.RuneError) {
		return string(out), "Big5"
	}
	out, _, _ := transform.Bytes(charmap.Windows1252.NewDecoder(), data)
	return string(out), "Windows-1252"
}

var unescaper = strings.NewReplacer(
	"__PIPE__", "|",
	"__DIEZE__", "#",
	"__PERCENT__", "%",
	"__PTVIRG__", ";",
	"__DBLQUO__", `"`,
	"__QUOTE__", "'",
)

// Unescape reverses the file format's escaping of special characters.
func Unescape(s string) string { return unescaper.Replace(s) }

type entry struct {
	name  string
	value string
}

type section struct {
	folder    string
	hasSubRep bool
	entries   []entry
}

// Parse reads .ini / .mxtsessions bookmark content.
func Parse(data []byte) (*Result, error) {
	return parse(data, runtime.GOOS, os.Getenv)
}

func parse(data []byte, goos string, getenv func(string) string) (*Result, error) {
	text, enc := Decode(data)
	res := &Result{Encoding: enc}

	var sections []*section
	var cur *section
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := trimmed[1 : len(trimmed)-1]
			if strings.EqualFold(name, "Bookmarks") || strings.HasPrefix(strings.ToLower(name), "bookmarks_") {
				cur = &section{}
				sections = append(sections, cur)
			} else {
				cur = nil
			}
			continue
		}
		if cur == nil {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		switch strings.ToLower(k) {
		case "subrep":
			cur.folder = cleanFolder(Unescape(strings.TrimSpace(v)))
			cur.hasSubRep = true
		case "imgnum":
		default:
			cur.entries = append(cur.entries, entry{name: Unescape(k), value: v})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(sections) == 0 {
		return nil, errors.New("no [Bookmarks] section found — is this a session .ini or .mxtsessions file?")
	}

	folders := map[string]bool{}
	keyWarned := false
	for _, s := range sections {
		if s.folder != "" {
			folders[s.folder] = true
		}
		for _, e := range s.entries {
			sess, typ, err := parseSession(e.name, e.value, goos, getenv)
			if err != nil {
				res.Skipped = append(res.Skipped, Skipped{Name: e.name, Folder: s.folder, Type: typ, Reason: err.Error()})
				continue
			}
			sess.Folder = s.folder
			if sess.SSH != nil && sess.SSH.KeyPath != "" && goos != "windows" && strings.Contains(sess.SSH.KeyPath, `\`) && !keyWarned {
				res.Warnings = append(res.Warnings, "Some private key paths point to Windows locations; update them in the session settings.")
				keyWarned = true
			}
			res.Sessions = append(res.Sessions, sess)
		}
	}
	for f := range folders {
		res.Folders = append(res.Folders, f)
	}
	sort.Strings(res.Folders)
	res.Warnings = append(res.Warnings, "Passwords are not imported (they are stored encrypted in the source file). You will be asked on first connect and can save them to the system keychain.")
	return res, nil
}

func cleanFolder(f string) string {
	return store.CleanFolder(strings.ReplaceAll(f, `\`, "/"))
}

// splitValue splits "#icon#type%f1%f2#terminal#..." and returns the type
// code and the '%' fields of the connection part (fields[0] is the type).
func splitValue(v string) (int, []string, error) {
	v = strings.TrimSpace(v)
	parts := strings.Split(v, "#")
	// parts[0] is empty (value starts with '#'), parts[1] icon, parts[2] connection
	if len(parts) < 3 || parts[0] != "" {
		return -1, nil, errors.New("unrecognised session format")
	}
	fields := strings.Split(parts[2], "%")
	t, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil {
		return -1, nil, errors.New("unrecognised session type")
	}
	return t, fields, nil
}

func field(f []string, i int) string {
	if i < len(f) {
		return strings.TrimSpace(f[i])
	}
	return ""
}

func parsePort(s string, def int) int {
	p, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || p <= 0 || p > 65535 {
		return def
	}
	return p
}

func parseSession(name, value, goos string, getenv func(string) string) (*store.Session, string, error) {
	typ, f, err := splitValue(value)
	if err != nil {
		return nil, "", err
	}
	tn := typeName(typ)
	switch typ {
	case typeSSH:
		host := Unescape(field(f, 1))
		if host == "" {
			return nil, tn, errors.New("missing host")
		}
		cfg := &store.SSHConfig{
			Host:           host,
			Port:           parsePort(field(f, 2), 22),
			User:           Unescape(field(f, 3)),
			Auth:           store.AuthAuto,
			StartupCommand: Unescape(field(f, 7)),
			SFTPAutoOpen:   true,
		}
		cfg.JumpHosts = parseGateways(field(f, 8), field(f, 9), field(f, 10))
		if key := Unescape(field(f, 14)); key != "" && key != "-1" && key != "0" {
			cfg.KeyPath = expandPath(key, goos, getenv)
			cfg.Auth = store.AuthKey
		}
		return &store.Session{Name: name, Type: store.TypeSSH, SSH: cfg}, tn, nil
	case typeTelnet:
		host := Unescape(field(f, 1))
		if host == "" {
			return nil, tn, errors.New("missing host")
		}
		return &store.Session{Name: name, Type: store.TypeTelnet, Telnet: &store.TelnetConfig{
			Host: host, Port: parsePort(field(f, 2), 23),
		}}, tn, nil
	case typeSerial:
		port := serialPort(Unescape(field(f, 1)))
		if port == "" {
			return nil, tn, errors.New("missing serial port")
		}
		cfg := &store.SerialConfig{
			Port:     port,
			DataBits: dataBits(field(f, 3)),
			Parity:   parity(field(f, 4)),
			StopBits: stopBits(field(f, 5)),
		}
		if b, err := strconv.Atoi(field(f, 2)); err == nil && b > 0 {
			cfg.Baud = b
		} else {
			cfg.Baud = 9600
		}
		return &store.Session{Name: name, Type: store.TypeSerial, Serial: cfg}, tn, nil
	case typeShell:
		lc := &store.LocalConfig{}
		switch sh := strings.ToLower(field(f, 1)); {
		case strings.Contains(sh, "powershell"):
			lc.Shell = "powershell.exe"
		case strings.Contains(sh, "cmd"):
			lc.Shell = "cmd.exe"
		}
		if goos != "windows" {
			lc.Shell = ""
		}
		return &store.Session{Name: name, Type: store.TypeLocal, Local: lc}, tn, nil
	default:
		return nil, tn, errors.New(skipReason(typ))
	}
}

// parseGateways maps SSH gateway fields (hops separated by
// __PIPE__) to inline jump hosts.
func parseGateways(hosts, ports, users string) []store.JumpHost {
	if hosts == "" {
		return nil
	}
	hs := strings.Split(hosts, "__PIPE__")
	ps := strings.Split(ports, "__PIPE__")
	us := strings.Split(users, "__PIPE__")
	var out []store.JumpHost
	for i, h := range hs {
		h = strings.TrimSpace(Unescape(h))
		if h == "" {
			continue
		}
		j := store.JumpHost{Host: h, Port: 22}
		if i < len(ps) {
			j.Port = parsePort(ps[i], 22)
		}
		if i < len(us) {
			j.User = strings.TrimSpace(Unescape(us[i]))
		}
		out = append(out, j)
	}
	return out
}

var (
	comRe = regexp.MustCompile(`(?i)\bCOM\d+\b`)
	devRe = regexp.MustCompile(`/dev/[^\s()]+`)
)

func serialPort(s string) string {
	if m := comRe.FindString(s); m != "" {
		return strings.ToUpper(m)
	}
	if m := devRe.FindString(s); m != "" {
		return m
	}
	return strings.TrimSpace(strings.SplitN(s, " ", 2)[0])
}

func dataBits(s string) int {
	n, err := strconv.Atoi(s)
	switch {
	case err != nil:
		return 8
	case n >= 5 && n <= 8:
		return n
	case n >= 0 && n <= 3:
		return n + 5
	}
	return 8
}

func parity(s string) string {
	switch s {
	case "1":
		return "odd"
	case "2":
		return "even"
	case "3":
		return "mark"
	case "4":
		return "space"
	}
	return "none"
}

func stopBits(s string) string {
	switch s {
	case "1":
		return "1.5"
	case "2":
		return "2"
	}
	return "1"
}

// expandPath resolves path placeholders used in the file.
func expandPath(p, goos string, getenv func(string) string) string {
	if goos != "windows" {
		return p
	}
	profile := getenv("USERPROFILE")
	drive := getenv("SystemDrive")
	if drive == "" {
		drive = "C:"
	}
	r := strings.NewReplacer(
		"_CurrentDrive_:", drive,
		"_CurrentDrive_", drive,
		"_ProfileDir_", profile,
		"_MyDocuments_", profile+`\Documents`,
		"%USERPROFILE%", profile,
		"%APPDATA%", getenv("APPDATA"),
	)
	return r.Replace(p)
}

// Summary is a short human description, e.g. for logs.
func (r *Result) Summary() string {
	return fmt.Sprintf("%d sessions, %d folders, %d skipped", len(r.Sessions), len(r.Folders), len(r.Skipped))
}
