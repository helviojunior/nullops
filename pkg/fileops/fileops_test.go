package fileops

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func newFS(t *testing.T) (*FS, string) {
	t.Helper()
	dir := t.TempDir()
	fs, err := New(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return fs, fs.Root()
}

func TestTraversalBlocked(t *testing.T) {
	fs, _ := newFS(t)
	for _, p := range []string{"../etc/passwd", "a/../../x"} {
		// cleaned against "/" these stay inside the root
		if ap, err := fs.rpath(p); err != nil || !within(fs.Root(), ap) {
			t.Fatalf("%q escaped: %v %v", p, ap, err)
		}
	}
	if _, err := fs.rpath("a\x00b"); err == nil {
		t.Fatal("NUL accepted")
	}
}

func TestSymlinkEscapeBlocked(t *testing.T) {
	fs, root := newFS(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("x"), 0o644)
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unsupported")
	}
	if _, err := fs.ReadText("link/secret.txt"); err == nil {
		t.Fatal("read through escaping symlink")
	}
	if _, err := fs.List("link"); err == nil {
		t.Fatal("listed escaping symlink")
	}
	// deleting the link removes the link, never the target
	if err := fs.Delete("link"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Fatal("target removed through symlink")
	}
}

func TestCRUD(t *testing.T) {
	fs, _ := newFS(t)
	if _, err := fs.Mkdir("", "sub"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Touch("sub", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.WriteText("sub/a.txt", "hello"); err != nil {
		t.Fatal(err)
	}
	if s, _ := fs.ReadText("sub/a.txt"); s != "hello" {
		t.Fatalf("got %q", s)
	}
	if _, err := fs.Rename("sub/a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Copy("sub/b.txt", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Copy("sub", "sub"); err == nil {
		t.Fatal("copied a folder into itself")
	}
	l, err := fs.List("")
	if err != nil || l.DirCount != 1 || l.FileCount != 1 {
		t.Fatalf("list: %+v %v", l, err)
	}
	if err := fs.Delete(""); err == nil {
		t.Fatal("deleted root")
	}
	if _, err := fs.Mkdir("", "../x"); err == nil {
		t.Fatal("invalid name accepted")
	}
}

func TestZipRoundtripAndZipSlip(t *testing.T) {
	fs, root := newFS(t)
	fs.Mkdir("", "d")
	fs.WriteText("d/f.txt", "data")
	if _, err := fs.Zip([]string{"d"}, "", "a.zip"); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Extract("a.zip", true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "a", "d", "f.txt")); string(b) != "data" {
		t.Fatalf("extracted %q", b)
	}
	if _, err := fs.Tar([]string{"d"}, "", "a.tar.gz"); err != nil {
		t.Fatal(err)
	}

	// malicious archive
	evil := filepath.Join(root, "evil.zip")
	fh, _ := os.Create(evil)
	zw := zip.NewWriter(fh)
	w, _ := zw.Create("../../escape.txt")
	w.Write([]byte("pwn"))
	zw.Close()
	fh.Close()
	if _, err := fs.Extract("evil.zip", false); err == nil {
		t.Fatal("zip-slip accepted")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); err == nil {
		t.Fatal("file escaped root")
	}
}

func TestUploadChunks(t *testing.T) {
	fs, root := newFS(t)
	if _, _, err := fs.UploadChunk("", "u.bin", 0, []byte("abc"), false, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fs.UploadChunk("", "u.bin", 10, []byte("x"), false, nil); err == nil {
		t.Fatal("out-of-order chunk accepted")
	}
	e, n, err := fs.UploadChunk("", "u.bin", 3, []byte("def"), true, nil)
	if err != nil || n != 6 || e == nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "u.bin")); string(b) != "abcdef" {
		t.Fatalf("got %q", b)
	}
	if _, _, err := fs.UploadChunk("", "x.exe", 0, nil, true, []string{"txt"}); err == nil {
		t.Fatal("extension filter ignored")
	}
}
