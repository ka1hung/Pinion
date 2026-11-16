package editor

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ShellQuote quotes s for POSIX sh.
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SplitCommand splits a command line respecting single/double quotes and
// backslash escapes inside double quotes (Windows paths with backslashes
// outside quotes are kept verbatim).
func SplitCommand(s string) ([]string, error) {
	var (
		args  []string
		cur   strings.Builder
		inArg bool
		quote rune
	)
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			if r == '"' {
				quote = 0
			} else if r == '\\' && i+1 < len(rs) && (rs[i+1] == '"' || rs[i+1] == '\\') {
				i++
				cur.WriteRune(rs[i])
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inArg = true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote in command")
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, nil
}

// BuildCommand expands a template ("code -n {file}") into argv. If the
// template has no {file} placeholder the file is appended.
func BuildCommand(tmpl, file string) ([]string, error) {
	args, err := SplitCommand(tmpl)
	if err != nil {
		return nil, err
	}
	if len(args) == 0 {
		return nil, errors.New("empty editor command")
	}
	found := false
	for i, a := range args {
		if strings.Contains(a, "{file}") {
			args[i] = strings.ReplaceAll(a, "{file}", file)
			found = true
		}
	}
	if !found {
		args = append(args, file)
	}
	return args, nil
}

// DetectVSCode returns the path of the VS Code CLI, or "".
func DetectVSCode() string {
	if p, err := exec.LookPath("code"); err == nil {
		return p
	}
	var cands []string
	switch runtime.GOOS {
	case "darwin":
		cands = []string{
			"/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code",
			filepath.Join(os.Getenv("HOME"), "Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"),
		}
	case "windows":
		for _, base := range []string{os.Getenv("LOCALAPPDATA") + `\Programs`, os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if base == "" || base == `\Programs` {
				continue
			}
			cands = append(cands, filepath.Join(base, "Microsoft VS Code", "bin", "code.cmd"))
		}
	default:
		cands = []string{"/usr/bin/code", "/usr/share/code/bin/code", "/snap/bin/code"}
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// Command builds an exec.Cmd for argv, wrapping .cmd/.bat via cmd /c on Windows.
func Command(argv []string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(argv[0]))
		if ext == ".cmd" || ext == ".bat" {
			return exec.Command("cmd", append([]string{"/c", argv[0]}, argv[1:]...)...)
		}
	}
	return exec.Command(argv[0], argv[1:]...)
}
