package editor

import (
	"io"
	"os"
	"path/filepath"

	"github.com/pkg/sftp"
)

// FS is the file access a document needs.
type FS interface {
	Stat(p string) (os.FileInfo, error)
	ReadFile(p string) ([]byte, error)
	// WriteFile replaces the file content, keeping owner and mode.
	WriteFile(p string, data []byte) error
}

// LocalFS is the local disk.
type LocalFS struct{}

func (LocalFS) Stat(p string) (os.FileInfo, error) { return os.Stat(p) }
func (LocalFS) ReadFile(p string) ([]byte, error)  { return os.ReadFile(p) }

// WriteFile writes via temp file + rename, preserving the mode.
func (LocalFS) WriteFile(p string, data []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(p); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".pinion-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	_ = os.Chmod(name, mode)
	if err := os.Rename(name, p); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// RemoteFS is an SFTP connection.
type RemoteFS struct{ C *sftp.Client }

func (r RemoteFS) Stat(p string) (os.FileInfo, error) { return r.C.Stat(p) }

func (r RemoteFS) ReadFile(p string) ([]byte, error) {
	f, err := r.C.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// WriteFile truncates and rewrites in place (keeps owner/mode/hardlinks).
func (r RemoteFS) WriteFile(p string, data []byte) error {
	f, err := r.C.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
