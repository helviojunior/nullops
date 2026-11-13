package pathref

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func open(t *testing.T, file string) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRefsRoundTrip(t *testing.T) {
	file := filepath.Join(t.TempDir(), "db.sqlite")
	s := open(t, file)
	if s.Ref("") != "" || s.Ref("/") != "" {
		t.Fatal("root must have no ref")
	}
	refs := s.Refs("VMWare/ELK", "/VMWare/ELK/", "docs/relatório #1.txt")
	if refs[0] != refs[1] {
		t.Fatal("same path, different refs")
	}
	for _, r := range refs {
		if !Valid(r) {
			t.Fatalf("invalid ref %q", r)
		}
	}
	if refs[0] == refs[2] {
		t.Fatal("collision")
	}
	if p, err := s.Resolve(refs[2]); err != nil || p != "docs/relatório #1.txt" {
		t.Fatalf("resolve: %q %v", p, err)
	}
	if _, err := s.Resolve("AAAAAAAAAAAAAAAA"); err == nil {
		t.Fatal("unknown ref resolved")
	}
	if _, err := s.Resolve("../etc"); err == nil {
		t.Fatal("bad ref resolved")
	}

	// a new process (same database): same key, same refs, resolves from disk
	s2 := open(t, file)
	if s2.Of("VMWare/ELK") != refs[0] {
		t.Fatal("key not persisted")
	}
	if p, err := s2.Resolve(refs[0]); err != nil || p != "VMWare/ELK" {
		t.Fatalf("resolve after restart: %q %v", p, err)
	}

	// another database: another key
	other := open(t, filepath.Join(t.TempDir(), "other.sqlite"))
	if other.Of("VMWare/ELK") == refs[0] {
		t.Fatal("refs must depend on the server key")
	}

	s2.Purge(time.Now().Add(time.Hour))
	s3 := open(t, file)
	if _, err := s3.Resolve(refs[0]); err == nil {
		t.Fatal("purged ref resolved")
	}
}
