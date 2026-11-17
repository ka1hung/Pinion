// Package fonts manages font files the user imports into Pinion.
//
// Imported fonts live in <config dir>/fonts and are served to the webview at
// /userfonts/<file>, where the frontend registers them with the FontFace API.
// They are only visible inside Pinion; nothing is installed into the OS.
package fonts

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/image/font/sfnt"
)

// URLPrefix is the asset path imported fonts are served under.
const URLPrefix = "/userfonts/"

// MaxSize caps imported font files (large CJK fonts are ~20-30 MB).
const MaxSize = 64 << 20

var mimeTypes = map[string]string{
	".ttf":   "font/ttf",
	".otf":   "font/otf",
	".woff":  "font/woff",
	".woff2": "font/woff2",
}

// Extensions lists importable file extensions.
func Extensions() []string { return []string{".ttf", ".otf", ".woff", ".woff2"} }

// Font is one imported font file.
type Font struct {
	File   string `json:"file"`   // file name inside the fonts dir
	Family string `json:"family"` // CSS family name to register it under
	Weight string `json:"weight"` // CSS font-weight, e.g. "400", "700"
	Style  string `json:"style"`  // "normal" | "italic"
	URL    string `json:"url"`    // where the webview can load it
}

// Dir returns the fonts directory under the config dir.
func Dir(configDir string) string { return filepath.Join(configDir, "fonts") }

func supported(name string) bool {
	_, ok := mimeTypes[strings.ToLower(filepath.Ext(name))]
	return ok
}

// List returns the imported fonts, sorted by family then file name.
func List(dir string) ([]Font, error) {
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Font{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Font{}
	for _, e := range ents {
		if e.IsDir() || !supported(e.Name()) {
			continue
		}
		out = append(out, describe(filepath.Join(dir, e.Name())))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Family != out[j].Family {
			return strings.ToLower(out[i].Family) < strings.ToLower(out[j].Family)
		}
		return out[i].File < out[j].File
	})
	return out, nil
}

// Import copies src into dir and returns its description. An existing file
// with the same name is replaced.
func Import(dir, src string) (Font, error) {
	name := filepath.Base(src)
	if !supported(name) {
		return Font{}, fmt.Errorf("unsupported font type %q (use %s)", filepath.Ext(name), strings.Join(Extensions(), ", "))
	}
	in, err := os.Open(src)
	if err != nil {
		return Font{}, err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return Font{}, err
	}
	if st.Size() > MaxSize {
		return Font{}, fmt.Errorf("font file is too large (%d MB, max %d MB)", st.Size()>>20, MaxSize>>20)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".ttf" || ext == ".otf" {
		b, err := io.ReadAll(in)
		if err != nil {
			return Font{}, err
		}
		if _, err := sfnt.Parse(b); err != nil {
			return Font{}, fmt.Errorf("not a valid font file: %w", err)
		}
		if _, err := in.Seek(0, io.SeekStart); err != nil {
			return Font{}, err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Font{}, err
	}
	dst := filepath.Join(dir, name)
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return Font{}, err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return Font{}, err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return Font{}, err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return Font{}, err
	}
	return describe(dst), nil
}

// Remove deletes an imported font file by name.
func Remove(dir, file string) error {
	name := filepath.Base(file)
	if name != file || !supported(name) {
		return fmt.Errorf("invalid font file %q", file)
	}
	return os.Remove(filepath.Join(dir, name))
}

// Handler serves dir at URLPrefix and passes every other request to next.
func Handler(dir string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, URLPrefix) {
			next.ServeHTTP(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, URLPrefix)
		if name == "" || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) || !supported(name) {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", mimeTypes[strings.ToLower(filepath.Ext(name))])
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, name, st.ModTime(), f)
	})
}

// describe reads family/weight/style from the font's name table, falling back
// to the file name for formats we can't parse (woff/woff2) or broken files.
func describe(path string) Font {
	name := filepath.Base(path)
	f := Font{File: name, Weight: "400", Style: "normal", URL: URLPrefix + name}
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	sub := stem
	if b, err := os.ReadFile(path); err == nil {
		if fnt, err := sfnt.Parse(b); err == nil {
			var buf sfnt.Buffer
			for _, id := range []sfnt.NameID{sfnt.NameIDTypographicFamily, sfnt.NameIDFamily} {
				if v, err := fnt.Name(&buf, id); err == nil && strings.TrimSpace(v) != "" {
					f.Family = strings.TrimSpace(v)
					break
				}
			}
			for _, id := range []sfnt.NameID{sfnt.NameIDTypographicSubfamily, sfnt.NameIDSubfamily} {
				if v, err := fnt.Name(&buf, id); err == nil && strings.TrimSpace(v) != "" {
					sub = v
					break
				}
			}
		}
	}
	if f.Family == "" {
		f.Family = familyFromStem(stem)
	}
	f.Weight, f.Style = weightStyle(sub)
	return f
}

var weights = []struct {
	key string
	w   string
}{
	{"extralight", "200"}, {"ultralight", "200"}, {"semibold", "600"}, {"demibold", "600"},
	{"extrabold", "800"}, {"ultrabold", "800"}, {"thin", "100"}, {"light", "300"},
	{"medium", "500"}, {"bold", "700"}, {"black", "900"}, {"heavy", "900"},
}

// weightStyle maps a subfamily like "Bold Italic" to CSS weight and style.
func weightStyle(sub string) (string, string) {
	s := strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "").Replace(sub))
	style := "normal"
	if strings.Contains(s, "italic") || strings.Contains(s, "oblique") {
		style = "italic"
	}
	for _, x := range weights {
		if strings.Contains(s, x.key) {
			return x.w, style
		}
	}
	return "400", style
}

// familyFromStem turns "JetBrainsMono-BoldItalic" into "JetBrainsMono".
func familyFromStem(stem string) string {
	if i := strings.LastIndexAny(stem, "-_"); i > 0 {
		w, st := weightStyle(stem[i+1:])
		if w != "400" || st != "normal" || strings.EqualFold(stem[i+1:], "regular") {
			return stem[:i]
		}
	}
	return stem
}
