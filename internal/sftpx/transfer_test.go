package sftpx

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
)

type rwc struct {
	io.Reader
	io.WriteCloser
}

func pipeClient(t *testing.T) *sftp.Client {
	t.Helper()
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()
	srv, err := sftp.NewServer(rwc{sr, sw})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve()
	cl, err := sftp.NewClientPipe(cr, cw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cw.Close()
		sw.Close()
		cr.Close()
		sr.Close()
		cl.Close()
		srv.Close()
	})
	return cl
}

func TestUploadDownloadRecursive(t *testing.T) {
	cl := pipeClient(t)
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "conf", "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "conf", "a.txt"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(src, "conf", "sub", "b.bin"), make([]byte, 100000), 0o600)
	remote := filepath.ToSlash(t.TempDir()) // the test server serves the local FS

	p := NewProgress("x1", "t1", "upload")
	if err := Upload(context.Background(), cl, p, []string{filepath.Join(src, "conf")}, remote); err != nil {
		t.Fatal(err)
	}
	s := p.Snapshot()
	if s.Files != 2 || s.FilesDone != 2 || s.Bytes != 100005 || s.TotalBytes != 100005 {
		t.Fatalf("%+v", s)
	}
	if b, _ := os.ReadFile(filepath.Join(remote, "conf", "a.txt")); string(b) != "hello" {
		t.Fatalf("uploaded %q", b)
	}

	dst := t.TempDir()
	p = NewProgress("x2", "t1", "download")
	if err := Download(context.Background(), cl, p, []string{remote + "/conf"}, dst); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(dst, "conf", "sub", "b.bin")); err != nil || st.Size() != 100000 {
		t.Fatalf("%v %v", st, err)
	}
	if p.Snapshot().FilesDone != 2 {
		t.Fatalf("%+v", p.Snapshot())
	}

	// cancelled transfer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p = NewProgress("x3", "t1", "download")
	if err := Download(ctx, cl, p, []string{remote + "/conf"}, t.TempDir()); err == nil {
		t.Fatal("expected cancellation error")
	}
}
