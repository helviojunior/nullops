package bundle

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPackOpen(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "assets"), 0o755)
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>SECRET-MARKER"), 0o644)
	os.WriteFile(filepath.Join(dir, "assets", "a.js"), []byte("console.log(1)"), 0o644)
	blob, n, err := PackDir(dir)
	if err != nil || n != 2 {
		t.Fatal(err, n)
	}
	if bytes.Contains(blob, []byte("SECRET-MARKER")) || bytes.Contains(blob, []byte("index.html")) {
		t.Fatal("plaintext visible in blob")
	}
	files, err := Open(blob)
	if err != nil {
		t.Fatal(err)
	}
	if string(files["assets/a.js"]) != "console.log(1)" {
		t.Fatal("roundtrip mismatch")
	}
	blob[len(blob)-1] ^= 1
	if _, err := Open(blob); err == nil {
		t.Fatal("tampered blob accepted")
	}
}
