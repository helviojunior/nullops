package session

import (
	"crypto/ecdh"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) (*SQLiteStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, path
}

func TestPersistAcrossRestart(t *testing.T) {
	st, path := newStore(t)
	m := NewManager(time.Hour, st, false)
	client, _ := ecdh.P256().GenerateKey(rand.Reader)
	s, _, err := m.Create(client.PublicKey().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Authenticate(s, "bob", true, "127.0.0.1", "ua"); err != nil {
		t.Fatal(err)
	}
	env := s.Cipher.Seal([]byte("hello"))

	// "restart": a new manager on the same database
	m2 := NewManager(time.Hour, st, false)
	s2 := m2.Get(s.ID)
	if s2 == nil {
		t.Fatal("session not restored")
	}
	ok, user, ro := s2.Auth()
	if !ok || user != "bob" || !ro {
		t.Fatalf("auth state lost: %v %q %v", ok, user, ro)
	}
	if pt, err := s2.Cipher.Open(env); err != nil || string(pt) != "hello" {
		t.Fatal("restored key does not decrypt")
	}

	// the raw id and key never hit the disk
	raw, _ := os.ReadFile(path)
	walRaw, _ := os.ReadFile(path + "-wal")
	all := string(raw) + string(walRaw)
	if strings.Contains(all, s.ID) {
		t.Fatal("session id stored in clear")
	}
	if strings.Contains(all, string(s.Cipher.Key())) {
		t.Fatal("session key stored in clear")
	}

	// a plaintext server must not accept an encrypted session
	if NewManager(time.Hour, st, true).Get(s.ID) != nil {
		t.Fatal("mode mismatch accepted")
	}
}

func TestPreAuthNotPersisted(t *testing.T) {
	st, _ := newStore(t)
	m := NewManager(time.Hour, st, true)
	s, _ := m.CreatePlain()
	if NewManager(time.Hour, st, true).Get(s.ID) != nil {
		t.Fatal("unauthenticated session persisted")
	}
}

func TestExpiryAndDestroy(t *testing.T) {
	st, _ := newStore(t)
	m := NewManager(time.Hour, st, true)
	s, _ := m.CreatePlain()
	m.Authenticate(s, "alice", false, "", "")
	m.Destroy(s.ID)
	if NewManager(time.Hour, st, true).Get(s.ID) != nil {
		t.Fatal("destroyed session restored")
	}

	m3 := NewManager(time.Second, st, true)
	s3, _ := m3.CreatePlain()
	m3.Authenticate(s3, "alice", false, "", "")
	rec, _ := st.Load(HashID(s3.ID))
	rec.ExpiresAt = time.Now().Add(-time.Minute)
	st.Save(rec)
	if NewManager(time.Hour, st, true).Get(s3.ID) != nil {
		t.Fatal("expired session restored")
	}
	if m.Get("not-a-valid-id") != nil {
		t.Fatal("bogus id accepted")
	}
}
