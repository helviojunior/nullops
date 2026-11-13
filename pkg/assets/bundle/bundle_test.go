package bundle

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestPerBuildPepper(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)
	if err := SetPepper(strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	blob, _, _ := PackDir(dir)
	if _, err := Open(blob); err != nil {
		t.Fatal("same pepper must open:", err)
	}
	SetPepper("")
	if _, err := Open(blob); err == nil {
		t.Fatal("different pepper opened the blob")
	}
	if err := SetPepper("zz"); err == nil {
		t.Fatal("invalid pepper accepted")
	}
}
