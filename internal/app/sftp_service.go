package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"

	"github.com/ka1hung/pinion/internal/sftpx"
	"github.com/ka1hung/pinion/internal/store"
)

var (
	sftpMgrMu sync.Mutex
	sftpMgrs  = map[*Core]*sftpx.Manager{}
)

// sftpManager returns the per-Core SFTP client manager (shared by the SFTP
// panel and the editor).
func sftpManager(c *Core) *sftpx.Manager {
	sftpMgrMu.Lock()
	defer sftpMgrMu.Unlock()
	if m := sftpMgrs[c]; m != nil {
		return m
	}
	m := sftpx.New(c.Pool, func(tabID string) (*store.Session, error) {
		t, err := c.Tabs.Get(tabID)
		if err != nil {
			return nil, err
		}
		return t.Session(), nil
	})
	c.Tabs.OnClose(m.Drop)
	c.OnShutdown(m.CloseAll)
	sftpMgrs[c] = m
	return m
}

// SFTPService is the SFTP side panel of an SSH tab.
type SFTPService struct {
	c   *Core
	m   *sftpx.Manager
	seq atomic.Uint64

	mu        sync.Mutex
	transfers map[string]context.CancelFunc
}

// NewSFTPService creates the service.
func NewSFTPService(c *Core) *SFTPService {
	return &SFTPService{c: c, m: sftpManager(c), transfers: map[string]context.CancelFunc{}}
}

// FileEntry is one remote directory entry.
type FileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	Perm       uint32 `json:"perm"`
	IsDir      bool   `json:"isDir"`
	IsLink     bool   `json:"isLink"`
	LinkTarget string `json:"linkTarget"`
	Mtime      int64  `json:"mtime"`
	Owner      string `json:"owner"`
	Group      string `json:"group"`
}

func (s *SFTPService) client(ctx context.Context, tabID string) (*sftp.Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cl, _, err := s.m.Client(ctx, tabID)
	return cl, err
}

func (s *SFTPService) changed(tabID, dir string) {
	s.c.Emit("sftp:changed", map[string]string{"tabId": tabID, "dir": dir})
}

func toEntry(dir string, fi os.FileInfo) FileEntry {
	e := FileEntry{
		Name: fi.Name(), Path: path.Join(dir, fi.Name()), Size: fi.Size(),
		Mode: fi.Mode().String(), Perm: uint32(fi.Mode().Perm()), IsDir: fi.IsDir(),
		IsLink: fi.Mode()&os.ModeSymlink != 0, Mtime: fi.ModTime().UnixMilli(),
	}
	if st, ok := fi.Sys().(*sftp.FileStat); ok {
		e.Owner, e.Group = strconv.FormatUint(uint64(st.UID), 10), strconv.FormatUint(uint64(st.GID), 10)
	}
	return e
}

// Home returns the remote working directory.
func (s *SFTPService) Home(ctx context.Context, tabID string) (string, error) {
	cl, err := s.client(ctx, tabID)
	if err != nil {
		return "", err
	}
	return cl.Getwd()
}

// List returns a directory listing (directories first).
func (s *SFTPService) List(ctx context.Context, tabID, dir string) ([]FileEntry, error) {
	cl, err := s.client(ctx, tabID)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		if dir, err = cl.Getwd(); err != nil {
			return nil, err
		}
	}
	infos, err := cl.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]FileEntry, 0, len(infos))
	for _, fi := range infos {
		e := toEntry(dir, fi)
		if e.IsLink {
			if t, err := cl.ReadLink(e.Path); err == nil {
				e.LinkTarget = t
			}
			if st, err := cl.Stat(e.Path); err == nil {
				e.IsDir = st.IsDir()
				if !e.IsDir {
					e.Size = st.Size()
				}
			}
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Stat returns info about one path.
func (s *SFTPService) Stat(ctx context.Context, tabID, p string) (*FileEntry, error) {
	cl, err := s.client(ctx, tabID)
	if err != nil {
		return nil, err
	}
	fi, err := cl.Stat(p)
	if err != nil {
		return nil, err
	}
	e := toEntry(path.Dir(p), fi)
	e.Path = p
	return &e, nil
}

// Mkdir creates a directory (and parents).
func (s *SFTPService) Mkdir(ctx context.Context, tabID, p string) error {
	cl, err := s.client(ctx, tabID)
	if err != nil {
		return err
	}
	if err := cl.MkdirAll(p); err != nil {
		return err
	}
	s.changed(tabID, path.Dir(p))
	return nil
}

// NewFile creates an empty file (fails if it exists).
func (s *SFTPService) NewFile(ctx context.Context, tabID, p string) error {
	cl, err := s.client(ctx, tabID)
	if err != nil {
		return err
	}
	f, err := cl.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	s.changed(tabID, path.Dir(p))
	return nil
}

// Rename moves a file or directory.
func (s *SFTPService) Rename(ctx context.Context, tabID, from, to string) error {
	cl, err := s.client(ctx, tabID)
	if err != nil {
		return err
	}
	if err := cl.PosixRename(from, to); err != nil {
		if err2 := cl.Rename(from, to); err2 != nil {
			return err2
		}
	}
	s.changed(tabID, path.Dir(from))
	if path.Dir(to) != path.Dir(from) {
		s.changed(tabID, path.Dir(to))
	}
	return nil
}

// Remove deletes files and directories recursively.
func (s *SFTPService) Remove(ctx context.Context, tabID string, paths []string) error {
	cl, err := s.client(ctx, tabID)
	if err != nil {
		return err
	}
	var errs []error
	dirs := map[string]bool{}
	for _, p := range paths {
		if p == "" || p == "/" {
			errs = append(errs, errors.New("refusing to delete /"))
			continue
		}
		fi, err := cl.Lstat(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if fi.IsDir() {
			err = cl.RemoveAll(p)
		} else {
			err = cl.Remove(p)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
		}
		dirs[path.Dir(p)] = true
	}
	for d := range dirs {
		s.changed(tabID, d)
	}
	return errors.Join(errs...)
}

// Chmod sets permissions from an octal string like "644" or "0755".
func (s *SFTPService) Chmod(ctx context.Context, tabID, p, mode string) error {
	m, err := strconv.ParseUint(strings.TrimSpace(mode), 8, 32)
	if err != nil || m > 0o7777 {
		return fmt.Errorf("invalid mode %q", mode)
	}
	cl, err := s.client(ctx, tabID)
	if err != nil {
		return err
	}
	fm := os.FileMode(m & 0o777)
	if m&0o4000 != 0 {
		fm |= os.ModeSetuid
	}
	if m&0o2000 != 0 {
		fm |= os.ModeSetgid
	}
	if m&0o1000 != 0 {
		fm |= os.ModeSticky
	}
	if err := cl.Chmod(p, fm); err != nil {
		return err
	}
	s.changed(tabID, path.Dir(p))
	return nil
}

func (s *SFTPService) startTransfer(tabID, kind string, run func(ctx context.Context, cl *sftp.Client, p *sftpx.Progress) error, onDone func()) (string, error) {
	cl, err := s.client(context.Background(), tabID)
	if err != nil {
		return "", err
	}
	id := "x" + strconv.FormatUint(s.seq.Add(1), 10)
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.transfers[id] = cancel
	s.mu.Unlock()
	p := sftpx.NewProgress(id, tabID, kind)
	go func() {
		done := make(chan struct{})
		tickerDone := make(chan struct{})
		go func() {
			defer close(tickerDone)
			t := time.NewTicker(100 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					s.c.Emit("sftp:transfer", p.Snapshot())
				}
			}
		}()
		err := run(ctx, cl, p)
		close(done)
		<-tickerDone // no "running" update may arrive after the final state
		cancelled := ctx.Err() != nil
		p.Finish(err, cancelled)
		cancel()
		s.mu.Lock()
		delete(s.transfers, id)
		s.mu.Unlock()
		s.c.Emit("sftp:transfer", p.Snapshot())
		if onDone != nil {
			onDone()
		}
	}()
	s.c.Emit("sftp:transfer", p.Snapshot())
	return id, nil
}

// Upload copies local files/directories into remoteDir. Returns a transfer ID.
func (s *SFTPService) Upload(tabID string, localPaths []string, remoteDir string) (string, error) {
	if len(localPaths) == 0 {
		return "", nil
	}
	return s.startTransfer(tabID, "upload", func(ctx context.Context, cl *sftp.Client, p *sftpx.Progress) error {
		return sftpx.Upload(ctx, cl, p, localPaths, remoteDir)
	}, func() { s.changed(tabID, remoteDir) })
}

// UploadDialog lets the user pick local files, then uploads them.
func (s *SFTPService) UploadDialog(tabID, remoteDir string) (string, error) {
	files, err := s.c.PickFiles("Upload files")
	if err != nil || len(files) == 0 {
		return "", err
	}
	return s.Upload(tabID, files, remoteDir)
}

// Download asks for a local folder and downloads remotePaths into it.
func (s *SFTPService) Download(tabID string, remotePaths []string) (string, error) {
	dir, err := s.c.PickFolder("Download to folder")
	if err != nil || dir == "" {
		return "", err
	}
	return s.DownloadTo(tabID, remotePaths, dir)
}

// DownloadTo downloads remotePaths into localDir.
func (s *SFTPService) DownloadTo(tabID string, remotePaths []string, localDir string) (string, error) {
	if len(remotePaths) == 0 {
		return "", nil
	}
	return s.startTransfer(tabID, "download", func(ctx context.Context, cl *sftp.Client, p *sftpx.Progress) error {
		return sftpx.Download(ctx, cl, p, remotePaths, localDir)
	}, nil)
}

// CancelTransfer stops a running transfer.
func (s *SFTPService) CancelTransfer(id string) {
	s.mu.Lock()
	cancel := s.transfers[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// PasteConflicts lists names that already exist in destDir for a paste
// (so the UI can ask before overwriting).
func (s *SFTPService) PasteConflicts(ctx context.Context, tabID, mode string, sources []string, destDir string) ([]string, error) {
	cl, err := s.client(ctx, tabID)
	if err != nil {
		return nil, err
	}
	items, err := sftpx.PlanPaste(cl, mode, sources, destDir)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, it := range items {
		if it.Exists {
			out = append(out, path.Base(it.Dst))
		}
	}
	return out, nil
}

// Paste copies or moves remote files/folders into destDir on the same host.
// mode: "copy" | "move". Runs on the server (cp -a / mv) when a shell is
// available, otherwise with SFTP operations. Returns a transfer ID.
func (s *SFTPService) Paste(tabID, mode string, sources []string, destDir string, overwrite bool) (string, error) {
	if mode != "copy" && mode != "move" {
		return "", errors.New("bad mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	cl, conn, err := s.m.Client(ctx, tabID)
	cancel()
	if err != nil {
		return "", err
	}
	items, err := sftpx.PlanPaste(cl, mode, sources, destDir)
	if err != nil {
		return "", err
	}
	if !overwrite {
		for _, it := range items {
			if it.Exists {
				return "", fmt.Errorf("「%s」已存在", path.Base(it.Dst))
			}
		}
	}
	srcDirs := map[string]bool{}
	for _, it := range items {
		srcDirs[path.Dir(it.Src)] = true
	}
	return s.startTransfer(tabID, mode, func(ctx context.Context, cl *sftp.Client, p *sftpx.Progress) error {
		useShell := conn != nil
		for _, it := range items {
			p.StartItem(path.Base(it.Src), len(items))
			var err error
			if useShell {
				err = sftpx.RunShell(ctx, conn.Client, sftpx.ShellCommand(mode, it))
				if errors.Is(err, sftpx.ErrNoShell) {
					useShell = false
				}
			}
			if !useShell {
				err = sftpx.SFTPPaste(ctx, cl, p, mode, it)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", path.Base(it.Src), err)
			}
			p.ItemDone()
		}
		return nil
	}, func() {
		s.changed(tabID, path.Clean(destDir))
		if mode == "move" {
			for d := range srcDirs {
				s.changed(tabID, d)
			}
		}
	})
}
