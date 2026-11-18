package sharelink

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func open(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "db.sqlite"))
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

func TestCreateResolveRoundTrip(t *testing.T) {
	s := open(t)
	l, err := s.Create("docs/report.pdf", "report.pdf", "alice", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID(l.ID) || !ValidToken(l.Token) {
		t.Fatalf("bad id/token: %q %q", l.ID, l.Token)
	}
	got, err := s.Resolve(l.ID, l.Token, time.Now())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Path != "docs/report.pdf" || got.Name != "report.pdf" {
		t.Fatalf("unexpected link: %+v", got)
	}
}

func TestResolveRejects(t *testing.T) {
	s := open(t)
	l, err := s.Create("a.txt", "a.txt", "bob", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, id, token string
	}{
		{"bad id format", "not-a-uuid", l.Token},
		{"bad token format", l.ID, "xyz"},
		{"wrong token", l.ID, "00000000000000000000000000000000"},
		{"unknown id", "11111111-1111-1111-1111-111111111111", l.Token},
		{"empty token", l.ID, ""},
	}
	for _, c := range cases {
		if _, err := s.Resolve(c.id, c.token, time.Now()); err != ErrNotFound {
			t.Errorf("%s: expected ErrNotFound, got %v", c.name, err)
		}
	}
}

func TestResolveExpiredAndRevoked(t *testing.T) {
	s := open(t)
	l, err := s.Create("a.txt", "a.txt", "bob", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// expired: now is past expiry
	if _, err := s.Resolve(l.ID, l.Token, l.ExpiresAt.Add(time.Second)); err != ErrNotFound {
		t.Fatalf("expired: expected ErrNotFound, got %v", err)
	}
	// revoked
	if err := s.Revoke(l.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(l.ID, l.Token, time.Now()); err != ErrNotFound {
		t.Fatalf("revoked: expected ErrNotFound, got %v", err)
	}
	// revoking again is a no-op -> ErrNotFound
	if err := s.Revoke(l.ID); err != ErrNotFound {
		t.Fatalf("double revoke: expected ErrNotFound, got %v", err)
	}
}

func TestClampTTL(t *testing.T) {
	s := open(t)
	now := time.Now()
	long, _ := s.Create("a", "a", "u", 365*24*time.Hour)
	if d := long.ExpiresAt.Sub(now); d > MaxValidity+time.Minute {
		t.Fatalf("ttl not clamped to max: %v", d)
	}
	zero, _ := s.Create("b", "b", "u", 0)
	if d := zero.ExpiresAt.Sub(now); d < DefaultValidity-time.Minute || d > DefaultValidity+time.Minute {
		t.Fatalf("zero ttl not defaulted: %v", d)
	}
	short, _ := s.Create("c", "c", "u", time.Second)
	if d := short.ExpiresAt.Sub(now); d < MinValidity-time.Minute {
		t.Fatalf("ttl not clamped to min: %v", d)
	}
}

func TestListAndRecordDownload(t *testing.T) {
	s := open(t)
	l, _ := s.Create("dir/x.bin", "x.bin", "u", time.Hour)
	s.RecordDownload(l.ID, 2048)
	links, err := s.List("dir/x.bin", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Downloads != 1 || links[0].Bytes != 2048 {
		t.Fatalf("unexpected list: %+v", links)
	}
	// other path, scoped list is empty; all=true sees it
	if got, _ := s.List("other", false, time.Now()); len(got) != 0 {
		t.Fatalf("scoped list should be empty, got %d", len(got))
	}
	if got, _ := s.List("", true, time.Now()); len(got) != 1 {
		t.Fatalf("all list should have 1, got %d", len(got))
	}
}
