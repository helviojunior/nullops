package auth

import (
	"path/filepath"
	"testing"
	"time"
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

func TestTOTP(t *testing.T) {
	// RFC 6238 test vector (SHA1, secret "12345678901234567890", T=59 -> 94287082, 8 digits).
	// We use 6 digits: the last six digits of the 8-digit value.
	secret := b32.EncodeToString([]byte("12345678901234567890"))
	code, _ := TOTPCode(secret, time.Unix(59, 0))
	if code != "287082" {
		t.Fatalf("rfc vector: got %s", code)
	}
	now := time.Now()
	c, _ := TOTPCode(secret, now)
	if _, ok := VerifyTOTP(secret, c, now); !ok {
		t.Fatal("current code rejected")
	}
	prev, _ := TOTPCode(secret, now.Add(-30*time.Second))
	if _, ok := VerifyTOTP(secret, prev, now); !ok {
		t.Fatal("previous step (skew) rejected")
	}
	old, _ := TOTPCode(secret, now.Add(-5*time.Minute))
	if _, ok := VerifyTOTP(secret, old, now); ok && old != c {
		t.Fatal("old code accepted")
	}
}

func TestMFAReplayAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	s, _ := Open(path)
	s.Set("eve", "password1", false, false)
	secret, _ := NewTOTPSecret()
	if err := s.SetMFA("eve", secret); err != nil {
		t.Fatal(err)
	}
	code, _ := TOTPCode(secret, time.Now())
	if !s.VerifyMFA("eve", code) {
		t.Fatal("valid code rejected")
	}
	if s.VerifyMFA("eve", code) {
		t.Fatal("replayed code accepted")
	}

	// a second process (CLI) changes the file: the running store sees it
	other, _ := Open(path)
	other.Set("frank", "password2", false, false)
	s.lastCheck = time.Time{}
	if _, ok := s.Get("frank"); !ok {
		t.Fatal("external change not reloaded")
	}
	other.Delete("frank")
	s.lastCheck = time.Time{}
	if _, ok := s.Get("frank"); ok {
		t.Fatal("external delete not reloaded")
	}
	if u, _ := s.Get("eve"); !u.HasMFA() {
		t.Fatal("mfa lost")
	}
}
