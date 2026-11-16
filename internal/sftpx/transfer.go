package sftpx

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/pkg/sftp"
)

// Transfer is the progress snapshot sent to the UI.
type Transfer struct {
	ID         string `json:"id"`
	TabID      string `json:"tabId"`
	Kind       string `json:"kind"` // upload | download
	Name       string `json:"name"`
	Files      int    `json:"files"`
	FilesDone  int    `json:"filesDone"`
	Bytes      int64  `json:"bytes"`
	TotalBytes int64  `json:"totalBytes"`
	State      string `json:"state"` // running | done | error | cancelled
	Error      string `json:"error"`
}

// Progress is a concurrency-safe transfer tracker.
type Progress struct {
	mu    sync.Mutex
	t     Transfer
	bytes atomic.Int64
}

// NewProgress creates a tracker.
func NewProgress(id, tabID, kind string) *Progress {
	return &Progress{t: Transfer{ID: id, TabID: tabID, Kind: kind, State: "running"}}
}

// Snapshot returns the current state.
func (p *Progress) Snapshot() Transfer {
	p.mu.Lock()
	defer p.mu.Unlock()
	t := p.t
	t.Bytes = p.bytes.Load()
	return t
}

func (p *Progress) setTotals(files int, bytes int64) {
	p.mu.Lock()
	p.t.Files, p.t.TotalBytes = files, bytes
	p.mu.Unlock()
}

func (p *Progress) startFile(name string) {
	p.mu.Lock()
	p.t.Name = name
	p.mu.Unlock()
}

func (p *Progress) fileDone() {
	p.mu.Lock()
	p.t.FilesDone++
	p.mu.Unlock()
}

// Finish sets the terminal state.
func (p *Progress) Finish(err error, cancelled bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case cancelled:
		p.t.State = "cancelled"
	case err != nil:
		p.t.State, p.t.Error = "error", err.Error()
	default:
		p.t.State = "done"
	}
}

type countingReader struct {
	ctx context.Context
	r   io.Reader
	p   *Progress
}

func (c *countingReader) Read(b []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.r.Read(b)
	c.p.bytes.Add(int64(n))
	return n, err
}

type countingWriter struct {
	ctx context.Context
	w   io.Writer
	p   *Progress
}

func (c *countingWriter) Write(b []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := c.w.Write(b)
	c.p.bytes.Add(int64(n))
	return n, err
}

type job struct {
	src, dst string
	size     int64
	mode     os.FileMode
	isDir    bool
}

// Upload copies local files/dirs into remoteDir.
func Upload(ctx context.Context, cl *sftp.Client, p *Progress, localPaths []string, remoteDir string) error {
	var jobs []job
	var total int64
	files := 0
	for _, lp := range localPaths {
		lp = filepath.Clean(lp)
		base := filepath.Dir(lp)
		err := filepath.WalkDir(lp, func(fp string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(base, fp)
			dst := path.Join(remoteDir, filepath.ToSlash(rel))
			info, err := d.Info()
			if err != nil {
				return err
			}
			if d.IsDir() {
				jobs = append(jobs, job{src: fp, dst: dst, isDir: true, mode: info.Mode().Perm()})
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			jobs = append(jobs, job{src: fp, dst: dst, size: info.Size(), mode: info.Mode().Perm()})
			total += info.Size()
			files++
			return nil
		})
		if err != nil {
			return err
		}
	}
	p.setTotals(files, total)
	for _, j := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if j.isDir {
			if err := cl.MkdirAll(j.dst); err != nil {
				return fmt.Errorf("mkdir %s: %w", j.dst, err)
			}
			continue
		}
		p.startFile(filepath.Base(j.src))
		if err := uploadFile(ctx, cl, p, j); err != nil {
			return err
		}
		p.fileDone()
	}
	return nil
}

func uploadFile(ctx context.Context, cl *sftp.Client, p *Progress, j job) error {
	src, err := os.Open(j.src)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := cl.OpenFile(j.dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("create %s: %w", j.dst, err)
	}
	_, err = dst.ReadFrom(&countingReader{ctx: ctx, r: src, p: p})
	cerr := dst.Close()
	if err != nil {
		return fmt.Errorf("upload %s: %w", j.dst, err)
	}
	if cerr != nil {
		return cerr
	}
	_ = cl.Chmod(j.dst, j.mode)
	return nil
}

// Download copies remote files/dirs into localDir.
func Download(ctx context.Context, cl *sftp.Client, p *Progress, remotePaths []string, localDir string) error {
	var jobs []job
	var total int64
	files := 0
	for _, rp := range remotePaths {
		rp = path.Clean(rp)
		base := path.Dir(rp)
		w := cl.Walk(rp)
		for w.Step() {
			if err := w.Err(); err != nil {
				return err
			}
			fp := w.Path()
			rel := fp[len(base):]
			if base == "/" {
				rel = fp[1:]
			}
			dst := filepath.Join(localDir, filepath.FromSlash(rel))
			st := w.Stat()
			if st.IsDir() {
				jobs = append(jobs, job{src: fp, dst: dst, isDir: true})
				continue
			}
			if !st.Mode().IsRegular() {
				continue
			}
			jobs = append(jobs, job{src: fp, dst: dst, size: st.Size(), mode: st.Mode().Perm()})
			total += st.Size()
			files++
		}
	}
	p.setTotals(files, total)
	for _, j := range jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if j.isDir {
			if err := os.MkdirAll(j.dst, 0o755); err != nil {
				return err
			}
			continue
		}
		p.startFile(path.Base(j.src))
		if err := downloadFile(ctx, cl, p, j); err != nil {
			return err
		}
		p.fileDone()
	}
	return nil
}

func downloadFile(ctx context.Context, cl *sftp.Client, p *Progress, j job) error {
	src, err := cl.Open(j.src)
	if err != nil {
		return fmt.Errorf("open %s: %w", j.src, err)
	}
	defer src.Close()
	if err := os.MkdirAll(filepath.Dir(j.dst), 0o755); err != nil {
		return err
	}
	mode := j.mode | 0o600
	dst, err := os.OpenFile(j.dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = src.WriteTo(&countingWriter{ctx: ctx, w: dst, p: p})
	cerr := dst.Close()
	if err != nil {
		_ = os.Remove(j.dst)
		return fmt.Errorf("download %s: %w", j.src, err)
	}
	return cerr
}
