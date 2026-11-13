// Package pathref maps file paths to opaque references, so the browser URL
// (/files/<ref>, /edit/<ref>, /view/<ref>) never shows a real path or file
// name. A ref is the HMAC-SHA256 of the path under a random server key
// (stable: the same path always gets the same ref, links can be shared and
// bookmarked), truncated to 16 base64url characters (96 bits). The path
// cannot be derived from the ref; the server keeps ref -> path in SQLite,
// with an in-memory cache in front of it.
package pathref

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"
)

// RefLen is the number of characters of a ref.
const RefLen = 16

// cacheMax bounds the memory cache; past it the cache starts over (the
// database still has everything).
const cacheMax = 200_000

// Retention: refs not seen for this long are purged (a listing that shows the
// path again recreates the same ref).
const Retention = 180 * 24 * time.Hour

var refRe = regexp.MustCompile(`^[A-Za-z0-9_-]{16}$`)

// Valid checks the ref format.
func Valid(ref string) bool { return refRe.MatchString(ref) }

// ErrNotFound is returned for unknown refs.
var ErrNotFound = errors.New("Not found")

const schema = `
CREATE TABLE IF NOT EXISTS path_refs (
	ref     TEXT PRIMARY KEY,
	path    TEXT    NOT NULL,
	seen_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS path_ref_key (
	id  INTEGER PRIMARY KEY CHECK (id = 1),
	k   BLOB    NOT NULL
);
`

// Store computes, records and resolves refs.
type Store struct {
	db  *sql.DB
	key []byte

	mu    sync.Mutex
	cache map[string]string // ref -> path, known to be in the database
}

// Open prepares the tables and loads (or creates) the HMAC key.
func Open(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	var key []byte
	err := db.QueryRow(`SELECT k FROM path_ref_key WHERE id = 1`).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && len(key) != 32) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if _, err := db.Exec(`INSERT OR REPLACE INTO path_ref_key (id, k) VALUES (1, ?)`, key); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return &Store{db: db, key: key, cache: map[string]string{}}, nil
}

// Clean normalizes a path the way the file API returns it ("a/b", root "").
func Clean(p string) string { return strings.Trim(p, "/") }

// Of returns the ref of path without recording it ("" for the root).
func (s *Store) Of(path string) string {
	path = Clean(path)
	if path == "" {
		return ""
	}
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(path))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))[:RefLen]
}

// Refs returns the refs of paths (same order) and records the new ones, in
// one transaction.
func (s *Store) Refs(paths ...string) []string {
	out := make([]string, len(paths))
	var todo []int
	s.mu.Lock()
	for i, p := range paths {
		r := s.Of(p)
		out[i] = r
		if r == "" {
			continue
		}
		if _, ok := s.cache[r]; !ok {
			todo = append(todo, i)
		}
	}
	s.mu.Unlock()
	if len(todo) == 0 {
		return out
	}
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return out
	}
	st, err := tx.Prepare(`INSERT INTO path_refs (ref, path, seen_at) VALUES (?, ?, ?)
		ON CONFLICT(ref) DO UPDATE SET path = excluded.path, seen_at = excluded.seen_at`)
	if err != nil {
		_ = tx.Rollback()
		return out
	}
	for _, i := range todo {
		if _, err := st.Exec(out[i], Clean(paths[i]), now); err != nil {
			_ = st.Close()
			_ = tx.Rollback()
			return out
		}
	}
	_ = st.Close()
	if tx.Commit() != nil {
		return out
	}
	s.mu.Lock()
	if len(s.cache)+len(todo) > cacheMax {
		s.cache = map[string]string{}
	}
	for _, i := range todo {
		s.cache[out[i]] = Clean(paths[i])
	}
	s.mu.Unlock()
	return out
}

// Ref returns the ref of one path, recording it.
func (s *Store) Ref(path string) string { return s.Refs(path)[0] }

// Resolve returns the path of ref.
func (s *Store) Resolve(ref string) (string, error) {
	if !Valid(ref) {
		return "", ErrNotFound
	}
	s.mu.Lock()
	p, ok := s.cache[ref]
	s.mu.Unlock()
	if ok {
		return p, nil
	}
	err := s.db.QueryRow(`SELECT path FROM path_refs WHERE ref = ?`, ref).Scan(&p)
	if err != nil {
		return "", ErrNotFound
	}
	// never trust a row that does not match the key (tampered database)
	if s.Of(p) != ref {
		return "", ErrNotFound
	}
	s.mu.Lock()
	if len(s.cache) < cacheMax {
		s.cache[ref] = p
	}
	s.mu.Unlock()
	return p, nil
}

// Purge removes refs not seen since before.
func (s *Store) Purge(before time.Time) {
	_, _ = s.db.Exec(`DELETE FROM path_refs WHERE seen_at < ?`, before.Unix())
}
