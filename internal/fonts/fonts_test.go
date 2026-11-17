package fonts

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
)

func TestImportListRemove(t *testing.T) {
	src := t.TempDir()
	dir := filepath.Join(t.TempDir(), "fonts")
	reg := filepath.Join(src, "GoMono.ttf")
	bold := filepath.Join(src, "GoMono-Bold.ttf")
	if err := os.WriteFile(reg, gomono.TTF, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bold, gomonobold.TTF, 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := Import(dir, reg)
	if err != nil {
		t.Fatal(err)
	}
	if f.Family != "Go Mono" || f.Weight != "400" || f.Style != "normal" || f.URL != "/userfonts/GoMono.ttf" {
		t.Fatalf("regular: %+v", f)
	}
	f, err = Import(dir, bold)
	if err != nil {
		t.Fatal(err)
	}
	if f.Family != "Go Mono" || f.Weight != "700" {
		t.Fatalf("bold: %+v", f)
	}

	list, err := List(dir)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %+v", err, list)
	}

	if err := Remove(dir, "GoMono-Bold.ttf"); err != nil {
		t.Fatal(err)
	}
	if err := Remove(dir, "../GoMono.ttf"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if list, _ := List(dir); len(list) != 1 {
		t.Fatalf("after remove: %+v", list)
	}
}

func TestImportRejects(t *testing.T) {
	src := t.TempDir()
	dir := t.TempDir()
	txt := filepath.Join(src, "a.txt")
	fake := filepath.Join(src, "fake.ttf")
	os.WriteFile(txt, []byte("x"), 0o600)
	os.WriteFile(fake, []byte("not a font"), 0o600)
	if _, err := Import(dir, txt); err == nil {
		t.Fatal("txt accepted")
	}
	if _, err := Import(dir, fake); err == nil {
		t.Fatal("invalid ttf accepted")
	}
}

func TestListMissingDir(t *testing.T) {
	list, err := List(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(list) != 0 {
		t.Fatalf("%v %+v", err, list)
	}
}

func TestHandler(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.ttf"), gomono.TTF, 0o600)
	os.WriteFile(filepath.Join(t.TempDir(), "secret.ttf"), []byte("s"), 0o600)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) })
	h := Handler(dir, next)

	cases := map[string]int{
		"/userfonts/a.ttf":           200,
		"/userfonts/missing.ttf":     404,
		"/userfonts/../secret.ttf":   404,
		"/userfonts/..%5Csecret.ttf": 404,
		"/userfonts/a.txt":           404,
		"/index.html":                299,
	}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("%s: got %d want %d", path, rec.Code, want)
		}
		if want == 200 && rec.Header().Get("Content-Type") != "font/ttf" {
			t.Errorf("%s: content-type %q", path, rec.Header().Get("Content-Type"))
		}
	}
}

func TestFamilyFromStem(t *testing.T) {
	for in, want := range map[string]string{
		"JetBrainsMono-BoldItalic": "JetBrainsMono",
		"Foo-Regular":              "Foo",
		"Sarasa-Mono-TC":           "Sarasa-Mono-TC",
		"Plain":                    "Plain",
	} {
		if got := familyFromStem(in); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}
