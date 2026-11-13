package fileops

import (
	"archive/zip"
	"bytes"
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
	id := "0123456789abcdef0123456789abcdef"
	up := func(off int64, data string, last bool) (*Entry, int64, error) {
		return fs.UploadChunk(UploadRequest{Name: "u.bin", ID: id, Offset: off, Total: 6, Data: []byte(data), Last: last})
	}
	if _, _, err := up(0, "abc", false); err != nil {
		t.Fatal(err)
	}
	if n, _ := fs.UploadStatus("", "u.bin", id); n != 3 {
		t.Fatalf("resume offset %d", n)
	}
	if l, _ := fs.List(""); len(l.Entries) != 0 {
		t.Fatal("part file listed")
	}
	if _, _, err := up(5, "x", false); err == nil {
		t.Fatal("out-of-order chunk accepted")
	}
	e, n, err := up(3, "def", true)
	if err != nil || n != 6 || e == nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "u.bin")); string(b) != "abcdef" {
		t.Fatalf("got %q", b)
	}
	if _, _, err := fs.UploadChunk(UploadRequest{Name: "x.exe", Total: 0, Last: true, AllowExt: []string{"txt"}}); err == nil {
		t.Fatal("extension filter ignored")
	}
	if _, _, err := fs.UploadChunk(UploadRequest{Name: "big", Total: 100, Data: []byte("a"), MaxSize: 10}); err == nil {
		t.Fatal("size limit ignored")
	}
	if _, _, err := fs.UploadChunk(UploadRequest{Name: "a", ID: "../../x", Last: true}); err == nil {
		t.Fatal("bad upload id accepted")
	}
}

func TestZipTo(t *testing.T) {
	fs, _ := newFS(t)
	fs.Mkdir("", "d")
	fs.WriteText("d/a.txt", "1")
	fs.WriteText("b.txt", "2")
	var buf bytes.Buffer
	if err := fs.ZipTo(&buf, []string{"d", "b.txt"}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if !names["d/a.txt"] || !names["b.txt"] {
		t.Fatalf("entries: %v", names)
	}
}

func TestDocumentEncodingsAndConflict(t *testing.T) {
	fs, root := newFS(t)
	// UTF-16LE with BOM and CRLF, as written by Windows PowerShell ISE
	ps := []byte{0xff, 0xfe}
	for _, r := range "Write-Host \"oi\"\r\nexit 0\r\n" {
		ps = append(ps, byte(r), 0)
	}
	os.WriteFile(filepath.Join(root, "s.ps1"), ps, 0o644)
	doc, err := fs.ReadDocument("s.ps1")
	if err != nil || doc.Encoding != "utf-16le" || doc.EOL != "crlf" || doc.Content != "Write-Host \"oi\"\nexit 0\n" {
		t.Fatalf("read: %+v %v", doc, err)
	}
	_, h2, err := fs.WriteDocument("s.ps1", doc.Content+"# x\n", doc.Encoding, doc.EOL, doc.Hash, false)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "s.ps1"))
	if raw[0] != 0xff || raw[1] != 0xfe || !bytesContainsUTF16(raw, "# x\r\n") {
		t.Fatalf("encoding/eol not preserved: % x", raw[:8])
	}
	// stale hash -> conflict; force overrides
	if _, _, err := fs.WriteDocument("s.ps1", "a\n", doc.Encoding, doc.EOL, doc.Hash, false); err == nil {
		t.Fatal("conflict not detected")
	}
	if _, _, err := fs.WriteDocument("s.ps1", "a\n", doc.Encoding, doc.EOL, h2, false); err != nil {
		t.Fatal("current hash rejected:", err)
	}
	if _, _, err := fs.WriteDocument("s.ps1", "b\n", "utf-8-bom", "lf", "deadbeef", true); err != nil {
		t.Fatal("force rejected:", err)
	}
	raw, _ = os.ReadFile(filepath.Join(root, "s.ps1"))
	if string(raw) != "\xef\xbb\xbfb\n" {
		t.Fatalf("utf-8 bom: %q", raw)
	}
}

func bytesContainsUTF16(raw []byte, s string) bool {
	var want []byte
	for _, r := range s {
		want = append(want, byte(r), 0)
	}
	return string(raw) != "" && containsBytes(raw, want)
}

func containsBytes(a, b []byte) bool {
	for i := 0; i+len(b) <= len(a); i++ {
		if string(a[i:i+len(b)]) == string(b) {
			return true
		}
	}
	return false
}
