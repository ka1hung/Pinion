package servers

import (
	"context"
	"crypto/subtle"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ka1hung/pinion/internal/privbind"
)

type httpServer struct {
	srv *http.Server
}

func (h *httpServer) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.srv.Shutdown(ctx); err != nil {
		return h.srv.Close()
	}
	return nil
}

type countingResponseWriter struct {
	http.ResponseWriter
	n int64
}

func (c *countingResponseWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	c.n += int64(n)
	return n, err
}

type fileHandler struct {
	m   *Manager
	cfg ServerConfig
}

func startHTTP(m *Manager, cfg ServerConfig, addr string) (stopper, error) {
	ln, err := privbind.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: &fileHandler{m: m, cfg: cfg}, ReadHeaderTimeout: 15 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return &httpServer{srv: srv}, nil
}

// NewHTTPHandler is exported for tests.
func NewHTTPHandler(m *Manager, cfg ServerConfig) http.Handler { return &fileHandler{m: m, cfg: cfg} }

func (h *fileHandler) logf(r *http.Request, action, file string, n int64, msg string) {
	h.m.log(LogEntry{Kind: KindHTTP, Remote: r.RemoteAddr, Action: action, File: file, Bytes: n, Message: msg})
}

func (h *fileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.m.touch(KindHTTP)
	if h.cfg.User != "" {
		u, p, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(u), []byte(h.cfg.User)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(h.cfg.Password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Pinion"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			if ok {
				h.logf(r, "error", r.URL.Path, 0, "bad credentials for "+u)
			}
			return
		}
	}
	if strings.Contains(r.URL.Path, "..") {
		// reject obvious traversal attempts outright (resolvePath would also)
		for _, seg := range strings.Split(strings.ReplaceAll(r.URL.Path, `\`, "/"), "/") {
			if seg == ".." {
				h.logf(r, "error", r.URL.Path, 0, "path traversal rejected")
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
	}
	p, err := resolvePath(h.cfg.Root, r.URL.Path)
	if err != nil {
		h.logf(r, "error", r.URL.Path, 0, err.Error())
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.get(w, r, p)
	case http.MethodPut:
		if !h.cfg.Writable {
			http.Error(w, "read-only server", http.StatusForbidden)
			return
		}
		h.put(w, r, p)
	case http.MethodPost:
		if !h.cfg.Writable {
			http.Error(w, "read-only server", http.StatusForbidden)
			return
		}
		h.post(w, r, p)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *fileHandler) get(w http.ResponseWriter, r *http.Request, p string) {
	st, err := os.Stat(p)
	if err != nil {
		h.logf(r, "error", r.URL.Path, 0, "not found")
		http.NotFound(w, r)
		return
	}
	if st.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		h.list(w, r, p)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.Error(w, "cannot open file", http.StatusInternalServerError)
		return
	}
	defer f.Close()
	cw := &countingResponseWriter{ResponseWriter: w}
	http.ServeContent(cw, r, st.Name(), st.ModTime(), f)
	if r.Method == http.MethodGet {
		h.logf(r, "read", r.URL.Path, cw.n, "")
	}
}

func (h *fileHandler) list(w http.ResponseWriter, r *http.Request, dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, "cannot read directory", http.StatusInternalServerError)
		return
	}
	sort.Slice(ents, func(i, j int) bool {
		if ents[i].IsDir() != ents[j].IsDir() {
			return ents[i].IsDir()
		}
		return strings.ToLower(ents[i].Name()) < strings.ToLower(ents[j].Name())
	})
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title := html.EscapeString(r.URL.Path)
	var b strings.Builder
	fmt.Fprintf(&b, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Index of %s</title>
<style>body{font:14px system-ui,sans-serif;margin:2em}table{border-collapse:collapse}td{padding:2px 12px}a{text-decoration:none}</style></head><body><h2>Index of %s</h2><table>`, title, title)
	if r.URL.Path != "/" {
		b.WriteString(`<tr><td><a href="../">../</a></td><td></td><td></td></tr>`)
	}
	for _, e := range ents {
		name := e.Name()
		info, err := e.Info()
		if err != nil {
			continue
		}
		size := fmt.Sprint(info.Size())
		if e.IsDir() {
			name += "/"
			size = "-"
		}
		fmt.Fprintf(&b, `<tr><td><a href="%s">%s</a></td><td align="right">%s</td><td>%s</td></tr>`,
			html.EscapeString((&url.URL{Path: name}).String()), html.EscapeString(name), size, info.ModTime().Format("2006-01-02 15:04"))
	}
	b.WriteString("</table>")
	if h.cfg.Writable {
		b.WriteString(`<hr><form method="post" enctype="multipart/form-data"><input type="file" name="file" multiple> <button>Upload</button></form>`)
	}
	b.WriteString("</body></html>")
	_, _ = io.WriteString(w, b.String())
	h.logf(r, "list", r.URL.Path, 0, "")
}

func saveFile(dst string, src io.Reader) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	tmp := dst + ".pinion-upload"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, src)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return n, err
	}
	return n, os.Rename(tmp, dst)
}

func (h *fileHandler) put(w http.ResponseWriter, r *http.Request, p string) {
	if strings.HasSuffix(r.URL.Path, "/") {
		http.Error(w, "PUT needs a file name", http.StatusBadRequest)
		return
	}
	n, err := saveFile(p, r.Body)
	if err != nil {
		h.logf(r, "error", r.URL.Path, n, err.Error())
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	h.logf(r, "write", r.URL.Path, n, "")
	w.WriteHeader(http.StatusCreated)
}

func (h *fileHandler) post(w http.ResponseWriter, r *http.Request, dir string) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		http.Error(w, "POST to a directory", http.StatusBadRequest)
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "expected multipart form", http.StatusBadRequest)
		return
	}
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "bad upload", http.StatusBadRequest)
			return
		}
		name := path.Base(strings.ReplaceAll(part.FileName(), `\`, "/"))
		if part.FormName() != "file" || name == "" || name == "." || name == "/" || name == ".." {
			continue
		}
		dst, err := resolvePath(dir, name)
		if err != nil {
			continue
		}
		rel := path.Join(r.URL.Path, name)
		n, err := saveFile(dst, part)
		if err != nil {
			h.logf(r, "error", rel, n, err.Error())
			http.Error(w, "upload failed", http.StatusInternalServerError)
			return
		}
		h.logf(r, "write", rel, n, "")
	}
	http.Redirect(w, r, r.URL.Path, http.StatusSeeOther)
}
