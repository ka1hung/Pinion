package sftpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// PasteItem is one source → target pair of a copy/move inside the remote host.
type PasteItem struct {
	Src, Dst string
	IsDir    bool
	Exists   bool // target already exists (will be replaced)
}

// PlanPaste resolves targets for copying/moving srcs into dstDir.
// Copies into the source's own folder get a "_copy" name; moving onto itself is skipped.
func PlanPaste(cl *sftp.Client, mode string, srcs []string, dstDir string) ([]PasteItem, error) {
	dstDir = path.Clean(dstDir)
	var out []PasteItem
	for _, src := range srcs {
		src = path.Clean(src)
		fi, err := cl.Lstat(src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", src, err)
		}
		if fi.IsDir() && (dstDir == src || strings.HasPrefix(dstDir, src+"/")) {
			return nil, fmt.Errorf("不能把資料夾「%s」放進它自己裡面", path.Base(src))
		}
		dst := path.Join(dstDir, path.Base(src))
		if dst == src {
			if mode == "move" {
				continue
			}
			dst = uniqueName(cl, dst)
		}
		_, err = cl.Lstat(dst)
		out = append(out, PasteItem{Src: src, Dst: dst, IsDir: fi.IsDir(), Exists: err == nil})
	}
	return out, nil
}

// uniqueName returns "name_copy.ext", "name_copy2.ext", … that does not exist.
func uniqueName(cl *sftp.Client, p string) string {
	dir, base := path.Split(p)
	ext := path.Ext(base)
	if strings.HasPrefix(base, ".") && strings.Count(base, ".") == 1 {
		ext = "" // dotfile like ".bashrc"
	}
	stem := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		suffix := "_copy"
		if i > 1 {
			suffix = fmt.Sprintf("_copy%d", i)
		}
		cand := dir + stem + suffix + ext
		if _, err := cl.Lstat(cand); err != nil {
			return cand
		}
	}
}

// ShellQuote quotes s for POSIX sh.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ErrNoShell means the server refused exec (e.g. an SFTP-only account).
var ErrNoShell = errors.New("remote shell not available")

// ShellCommand builds the server-side command for one item.
func ShellCommand(mode string, it PasteItem) string {
	q := ShellQuote
	pre := ""
	if it.Exists {
		pre = "rm -rf -- " + q(it.Dst) + " && "
	}
	if mode == "move" {
		return pre + "mv -f -- " + q(it.Src) + " " + q(it.Dst)
	}
	return pre + "cp -a -- " + q(it.Src) + " " + q(it.Dst)
}

// RunShell executes cmd over a new SSH session; returns ErrNoShell when the
// server does not allow exec or lacks the tools (exit 126/127).
func RunShell(ctx context.Context, client *ssh.Client, cmd string) error {
	s, err := client.NewSession()
	if err != nil {
		return ErrNoShell
	}
	defer s.Close()
	stop := context.AfterFunc(ctx, func() { _ = s.Signal(ssh.SIGKILL); _ = s.Close() })
	defer stop()
	out, err := s.CombinedOutput(cmd)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		if ee.ExitStatus() == 126 || ee.ExitStatus() == 127 {
			return ErrNoShell
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	// exec request refused / channel failure
	return ErrNoShell
}

// SFTPPaste performs one item with SFTP operations only (fallback).
func SFTPPaste(ctx context.Context, cl *sftp.Client, p *Progress, mode string, it PasteItem) error {
	if it.Exists {
		if err := removeAll(cl, it.Dst); err != nil {
			return err
		}
	}
	if mode == "move" {
		if err := cl.Rename(it.Src, it.Dst); err == nil {
			return nil
		}
		// rename across filesystems is not possible: copy then delete
		if err := copyTree(ctx, cl, p, it.Src, it.Dst); err != nil {
			return err
		}
		return removeAll(cl, it.Src)
	}
	return copyTree(ctx, cl, p, it.Src, it.Dst)
}

func copyTree(ctx context.Context, cl *sftp.Client, p *Progress, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fi, err := cl.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := cl.ReadLink(src)
		if err != nil {
			return err
		}
		return cl.Symlink(target, dst)
	case fi.IsDir():
		if err := cl.Mkdir(dst); err != nil {
			return err
		}
		entries, err := cl.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(ctx, cl, p, path.Join(src, e.Name()), path.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return cl.Chmod(dst, fi.Mode().Perm())
	default:
		in, err := cl.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := cl.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, &countingReader{ctx: ctx, r: in, p: p}); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		_ = cl.Chmod(dst, fi.Mode().Perm())
		return cl.Chtimes(dst, fi.ModTime(), fi.ModTime())
	}
}

func removeAll(cl *sftp.Client, p string) error {
	fi, err := cl.Lstat(p)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		entries, err := cl.ReadDir(p)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := removeAll(cl, path.Join(p, e.Name())); err != nil {
				return err
			}
		}
		return cl.RemoveDirectory(p)
	}
	return cl.Remove(p)
}

// StartItem / FinishItem expose progress hooks for callers outside this package.
func (p *Progress) StartItem(name string, total int) {
	p.setTotals(total, 0)
	p.startFile(name)
}

// ItemDone marks one item finished.
func (p *Progress) ItemDone() { p.fileDone() }
