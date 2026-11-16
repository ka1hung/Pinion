package sftpx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasteCopyMoveSFTP(t *testing.T) {
	cl := pipeClient(t)
	root := filepath.ToSlash(t.TempDir())
	os.MkdirAll(root+"/a/dir/sub", 0o755)
	os.WriteFile(root+"/a/dir/sub/f.txt", []byte("hello"), 0o640)
	os.WriteFile(root+"/a/x.conf", []byte("x"), 0o600)
	os.MkdirAll(root+"/b", 0o755)
	ctx := context.Background()

	// copy into the same folder gets a _copy name
	items, err := PlanPaste(cl, "copy", []string{root + "/a/x.conf"}, root+"/a")
	if err != nil || len(items) != 1 || items[0].Dst != root+"/a/x_copy.conf" {
		t.Fatalf("plan same-dir copy: %+v %v", items, err)
	}
	// recursive copy of a folder to b/
	items, _ = PlanPaste(cl, "copy", []string{root + "/a/dir"}, root+"/b")
	p := NewProgress("1", "t", "copy")
	if err := SFTPPaste(ctx, cl, p, "copy", items[0]); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(root + "/b/dir/sub/f.txt"); string(b) != "hello" {
		t.Fatalf("copied content %q", b)
	}
	if fi, _ := os.Stat(root + "/b/dir/sub/f.txt"); fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", fi.Mode())
	}
	// copying again reports a conflict
	items, _ = PlanPaste(cl, "copy", []string{root + "/a/dir"}, root+"/b")
	if !items[0].Exists {
		t.Fatal("expected conflict")
	}
	// move x.conf to b
	items, _ = PlanPaste(cl, "move", []string{root + "/a/x.conf"}, root+"/b")
	if err := SFTPPaste(ctx, cl, p, "move", items[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root + "/a/x.conf"); !os.IsNotExist(err) {
		t.Fatal("source still exists after move")
	}
	// a folder cannot go into itself
	if _, err := PlanPaste(cl, "move", []string{root + "/a"}, root+"/a/dir"); err == nil {
		t.Fatal("expected error moving folder into itself")
	}
	// moving onto itself is a no-op
	items, _ = PlanPaste(cl, "move", []string{root + "/b/x.conf"}, root+"/b")
	if len(items) != 0 {
		t.Fatal("move onto itself should be skipped")
	}
}

func TestShellCommandQuoting(t *testing.T) {
	c := ShellCommand("copy", PasteItem{Src: "/a/it's here", Dst: "/b/x y", Exists: true})
	if !strings.Contains(c, `'/a/it'\''s here'`) || !strings.HasPrefix(c, "rm -rf -- '/b/x y' && cp -a -- ") {
		t.Fatal(c)
	}
}
