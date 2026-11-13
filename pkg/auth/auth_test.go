package auth

import (
	"path/filepath"
	"testing"
)

func TestHashVerify(t *testing.T) {
	h, err := HashPassword("s3cret!")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("s3cret!", h) || VerifyPassword("wrong", h) {
		t.Fatal("verify mismatch")
	}
	// hash produced by Python's hashlib.scrypt (format used by the python branch)
	py := "scrypt$16384$8$1$00112233445566778899aabbccddeeff$" +
		"81c6368ccd16cdd1f845d0cf96e5c7344c9e7484c5c4d93ccc2de24c72cfb01d"
	if !VerifyPassword("pyhash", py) {
		t.Fatal("python-compatible hash rejected")
	}
	if VerifyPassword("x", "scrypt$99999999$8$1$00$00") {
		t.Fatal("absurd params accepted")
	}
}

func TestStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	if err := s.Set("bob", "password1", true, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("bob", "password2", false, false); err != ErrUserExists {
		t.Fatal("duplicate accepted")
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	u, ok := s2.Verify("bob", "password1")
	if !ok || !u.ReadOnly {
		t.Fatal("reload/verify failed")
	}
	if _, ok := s2.Verify("nobody", "x"); ok {
		t.Fatal("unknown user accepted")
	}
	if err := s.Set("bad/name", "password1", false, false); err == nil {
		t.Fatal("invalid username accepted")
	}
}
