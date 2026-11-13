package session

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite (no cgo: keeps cross-compiling simple)
)

// Record is a persisted authenticated session.
//
// The session id is never written: rows are keyed by HashID(id) and the AES
// key is wrapped (AES-256-GCM) with a key derived from the id. Whoever copies
// the database alone cannot recover the session keys or impersonate a session.
type Record struct {
	IDHash     string
	Username   string
	ReadOnly   bool
	Encrypted  bool
	WrappedKey []byte
	CreatedAt  time.Time
	ExpiresAt  time.Time
	IP         string
	UserAgent  string
}

// Store persists authenticated sessions.
type Store interface {
	Save(r *Record) error
	Load(idHash string) (*Record, error) // (nil, nil) when not found
	Delete(idHash string) error
	Purge(now time.Time) (int64, error)
	List() ([]*Record, error)
	DeleteUser(username string) (int64, error)
	DeletePrefix(idHashPrefix string) (int64, error)
	Close() error
}

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	id_hash     TEXT PRIMARY KEY,
	username    TEXT    NOT NULL,
	readonly    INTEGER NOT NULL,
	encrypted   INTEGER NOT NULL,
	wrapped_key BLOB,
	created_at  INTEGER NOT NULL,
	expires_at  INTEGER NOT NULL,
	ip          TEXT,
	user_agent  TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
`

// SQLiteStore is the SQLite-backed Store.
type SQLiteStore struct {
	db *sql.DB
}

// OpenSQLite opens (or creates) the session database at path (mode 0600).
func OpenSQLite(path string) (*SQLiteStore, error) {
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
		f.Close()
	}
	_ = os.Chmod(path, 0o600)
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLiteStore{db: db}, nil
}

// Save inserts or replaces a record.
func (s *SQLiteStore) Save(r *Record) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO sessions
		(id_hash, username, readonly, encrypted, wrapped_key, created_at, expires_at, ip, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.IDHash, r.Username, b2i(r.ReadOnly), b2i(r.Encrypted), r.WrappedKey,
		r.CreatedAt.Unix(), r.ExpiresAt.Unix(), r.IP, r.UserAgent)
	return err
}

// Load returns the record for idHash.
func (s *SQLiteStore) Load(idHash string) (*Record, error) {
	var r Record
	var ro, enc int
	var created, expires int64
	var ip, ua sql.NullString
	err := s.db.QueryRow(`SELECT id_hash, username, readonly, encrypted, wrapped_key,
		created_at, expires_at, ip, user_agent FROM sessions WHERE id_hash = ?`, idHash).
		Scan(&r.IDHash, &r.Username, &ro, &enc, &r.WrappedKey, &created, &expires, &ip, &ua)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.ReadOnly, r.Encrypted = ro != 0, enc != 0
	r.CreatedAt, r.ExpiresAt = time.Unix(created, 0), time.Unix(expires, 0)
	r.IP, r.UserAgent = ip.String, ua.String
	return &r, nil
}

// Delete removes a record.
func (s *SQLiteStore) Delete(idHash string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id_hash = ?`, idHash)
	return err
}

// Purge removes expired records.
func (s *SQLiteStore) Purge(now time.Time) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now.Unix())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// List returns every record, newest first.
func (s *SQLiteStore) List() ([]*Record, error) {
	rows, err := s.db.Query(`SELECT id_hash, username, readonly, encrypted, created_at, expires_at,
		ip, user_agent FROM sessions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Record
	for rows.Next() {
		var r Record
		var ro, enc int
		var created, expires int64
		var ip, ua sql.NullString
		if err := rows.Scan(&r.IDHash, &r.Username, &ro, &enc, &created, &expires, &ip, &ua); err != nil {
			return nil, err
		}
		r.ReadOnly, r.Encrypted = ro != 0, enc != 0
		r.CreatedAt, r.ExpiresAt = time.Unix(created, 0), time.Unix(expires, 0)
		r.IP, r.UserAgent = ip.String, ua.String
		out = append(out, &r)
	}
	return out, rows.Err()
}

// DeleteUser removes every session of username.
func (s *SQLiteStore) DeleteUser(username string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE username = ?`, username)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeletePrefix removes the sessions whose id hash starts with prefix (min 6 chars).
func (s *SQLiteStore) DeletePrefix(prefix string) (int64, error) {
	if len(prefix) < 6 {
		return 0, errors.New("session id prefix must have at least 6 characters")
	}
	for _, c := range prefix {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return 0, errors.New("invalid session id")
		}
	}
	res, err := s.db.Exec(`DELETE FROM sessions WHERE id_hash LIKE ?`, prefix+"%")
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DB exposes the database (shared with other stores, e.g. pads).
func (s *SQLiteStore) DB() *sql.DB { return s.db }

// Close closes the database.
func (s *SQLiteStore) Close() error { return s.db.Close() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---- key wrapping ---------------------------------------------------------

const wrapInfo = "nullops/session-wrap/v1"

func kek(id string) (cipher.AEAD, error) {
	raw, err := hex.DecodeString(id)
	if err != nil {
		return nil, err
	}
	k, err := hkdf.Key(sha256.New, raw, nil, wrapInfo, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func wrapKey(id string, key []byte) ([]byte, error) {
	a, err := kek(id)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, key, []byte(HashID(id))), nil
}

func unwrapKey(id string, wrapped []byte) ([]byte, error) {
	a, err := kek(id)
	if err != nil {
		return nil, err
	}
	if len(wrapped) < a.NonceSize() {
		return nil, errors.New("wrapped key too short")
	}
	return a.Open(nil, wrapped[:a.NonceSize()], wrapped[a.NonceSize():], []byte(HashID(id)))
}
