package pads

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newManager(t *testing.T) (*Manager, *Store, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "nullops.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	st, err := OpenStore(db, filepath.Join(dir, "pads"))
	if err != nil {
		t.Fatal(err)
	}
	return NewManager(st), st, dir
}

func ins(base int, at int, s string) *Operation {
	return (&Operation{}).Retain(at).Insert(FromString(s)).Retain(base - at)
}

func TestConcurrentEditsMerge(t *testing.T) {
	m, _, _ := newManager(t)
	if err := m.Create("notes", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := m.Create("notes", "bob"); err != ErrExists {
		t.Fatal("duplicate name accepted")
	}
	if err := m.Create("bad/name", "bob"); err != ErrInvalidName {
		t.Fatal("invalid name accepted")
	}
	m.Edit("notes", "alice", "A", 1, 0, ins(0, 0, "hello world"))
	// both edit revision 1 at the same time
	r1, err := m.Edit("notes", "alice", "A", 2, 1, ins(11, 5, ","))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := m.Edit("notes", "bob", "B", 1, 1, ins(11, 11, "!"))
	if err != nil {
		t.Fatal(err)
	}
	snap, _ := m.Open("notes", "carol", "C")
	if snap.Text != "hello, world!" || r1 != 2 || r2 != 3 || snap.Rev != 3 {
		t.Fatalf("got %q rev %d (%d %d)", snap.Text, snap.Rev, r1, r2)
	}
	// a retried request (same seq) is not applied twice
	if _, err := m.Edit("notes", "bob", "B", 1, 1, ins(11, 11, "!")); err != nil {
		t.Fatal(err)
	}
	if s, _ := m.Open("notes", "carol", "C"); s.Text != "hello, world!" {
		t.Fatalf("duplicate applied: %q", s.Text)
	}
	// delete overlapping with an insert
	m.Edit("notes", "alice", "A", 3, 3, (&Operation{}).Retain(5).Delete(1).Retain(7))
	m.Edit("notes", "bob", "B", 2, 3, (&Operation{}).Retain(6).Insert(FromString(" big")).Retain(7))
	if s, _ := m.Open("notes", "carol", "C"); s.Text != "hello big world!" {
		t.Fatalf("got %q", s.Text)
	}
}

func TestPollWakesAndAcks(t *testing.T) {
	m, _, _ := newManager(t)
	m.Create("p", "alice")
	done := make(chan *PollResult, 1)
	go func() {
		r, _ := m.Poll(context.Background(), "p", "bob", "B", 0, 5*time.Second)
		done <- r
	}()
	time.Sleep(50 * time.Millisecond)
	m.Edit("p", "alice", "A", 1, 0, ins(0, 0, "hi"))
	select {
	case r := <-done:
		if r.Rev != 1 || len(r.Changes) != 1 || r.Changes[0].Client != "A" {
			t.Fatalf("poll: %+v", r)
		}
		if len(r.Editors) != 2 {
			t.Fatalf("editors: %v", r.Editors)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("poll did not wake up")
	}
	// nothing new: returns empty after the wait
	r, _ := m.Poll(context.Background(), "p", "bob", "B", 1, 30*time.Millisecond)
	if len(r.Changes) != 0 || r.Rev != 1 {
		t.Fatalf("empty poll: %+v", r)
	}
	if _, err := m.Poll(context.Background(), "p", "bob", "B", 9, time.Millisecond); err != ErrBadRev {
		t.Fatal("future revision accepted")
	}
}

func TestPersistenceIsEncrypted(t *testing.T) {
	m, st, dir := newManager(t)
	m.Create("secret", "alice")
	m.Edit("secret", "alice", "A", 1, 0, ins(0, 0, "TOP-SECRET-CONTENT ç😀"))
	m.Flush()

	meta, _ := st.Get("secret")
	raw, err := os.ReadFile(filepath.Join(dir, "pads", meta.ID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "TOP-SECRET") {
		t.Fatal("pad stored in clear text")
	}
	// a fresh manager (server restart) reads it back
	m2 := NewManager(st)
	snap, err := m2.Open("secret", "bob", "B")
	if err != nil || snap.Text != "TOP-SECRET-CONTENT ç😀" || snap.Rev != 1 {
		t.Fatalf("reload: %+v %v", snap, err)
	}
	// an old revision (history lost at restart) asks the client to resync
	if _, err := m2.Edit("secret", "bob", "B", 1, 0, ins(0, 0, "x")); err != ErrResync {
		t.Fatalf("expected resync, got %v", err)
	}
	r, _ := m2.Poll(context.Background(), "secret", "bob", "B", 0, time.Millisecond)
	if r.Snapshot == nil || r.Snapshot.Text != "TOP-SECRET-CONTENT ç😀" {
		t.Fatalf("poll resync: %+v", r)
	}
	if err := m2.Delete("secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pads", meta.ID)); !os.IsNotExist(err) {
		t.Fatal("pad file left behind")
	}
}
