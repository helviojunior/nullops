// Package uploadreq implements upload requests: a signed-in user creates a
// public link (/public/upload/<token>) that lets anyone upload files into one
// folder for a limited time, without an account.
package uploadreq

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"regexp"
	"time"
)

const (
	// TokenLen is the number of characters of a token ([A-Za-z0-9]).
	TokenLen = 8
	// Validity is how long a link accepts new uploads.
	Validity = 3 * time.Hour
	// Grace lets an upload started before the expiry finish its last chunks.
	Grace = 2 * time.Hour
)

const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

var tokenRe = regexp.MustCompile(`^[A-Za-z0-9]{8}$`)

// ValidToken checks the token format.
func ValidToken(t string) bool { return tokenRe.MatchString(t) }

var (
	// ErrNotFound is returned for unknown, revoked or expired tokens.
	ErrNotFound = errors.New("This upload link is invalid or has expired")
)

// Request is a stored upload request.
type Request struct {
	Token     string    `json:"token"`
	Dir       string    `json:"path"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Uploads   int       `json:"uploads"`
	Bytes     int64     `json:"bytes"`
	LastUseAt time.Time `json:"last_used_at,omitempty"`
}

const schema = `
CREATE TABLE IF NOT EXISTS upload_requests (
	token       TEXT PRIMARY KEY,
	dir         TEXT    NOT NULL,
	created_by  TEXT    NOT NULL,
	created_at  INTEGER NOT NULL,
	expires_at  INTEGER NOT NULL,
	revoked     INTEGER NOT NULL DEFAULT 0,
	uploads     INTEGER NOT NULL DEFAULT 0,
	bytes       INTEGER NOT NULL DEFAULT 0,
	last_use_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_upload_requests_dir ON upload_requests(dir);
`

// Store keeps upload requests in SQLite.
type Store struct{ db *sql.DB }

// Open prepares the table.
func Open(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func newToken() (string, error) {
	b := make([]byte, TokenLen)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b), nil
}

// Create stores a new request for dir by user, valid for Validity.
func (s *Store) Create(dir, user string) (*Request, error) {
	now := time.Now()
	for attempt := 0; attempt < 5; attempt++ {
		tok, err := newToken()
		if err != nil {
			return nil, err
		}
		r := &Request{Token: tok, Dir: dir, CreatedBy: user, CreatedAt: now, ExpiresAt: now.Add(Validity)}
		_, err = s.db.Exec(`INSERT INTO upload_requests (token, dir, created_by, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?)`, tok, dir, user, now.Unix(), r.ExpiresAt.Unix())
		if err == nil {
			return r, nil
		}
	}
	return nil, errors.New("could not create a unique token")
}

func scan(row interface{ Scan(...any) error }) (*Request, bool, error) {
	var r Request
	var created, expires int64
	var revoked int
	var last sql.NullInt64
	if err := row.Scan(&r.Token, &r.Dir, &r.CreatedBy, &created, &expires, &revoked, &r.Uploads, &r.Bytes, &last); err != nil {
		return nil, false, err
	}
	r.CreatedAt, r.ExpiresAt = time.Unix(created, 0), time.Unix(expires, 0)
	if last.Valid {
		r.LastUseAt = time.Unix(last.Int64, 0)
	}
	return &r, revoked != 0, nil
}

const cols = `token, dir, created_by, created_at, expires_at, revoked, uploads, bytes, last_use_at`

// Get returns a request usable at time now (grace extends the expiry for
// uploads already in progress).
func (s *Store) Get(token string, now time.Time, grace time.Duration) (*Request, error) {
	if !ValidToken(token) {
		return nil, ErrNotFound
	}
	r, revoked, err := scan(s.db.QueryRow(`SELECT `+cols+` FROM upload_requests WHERE token = ?`, token))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (revoked || now.After(r.ExpiresAt.Add(grace)))) {
		return nil, ErrNotFound
	}
	return r, err
}

// List returns the active requests (optionally for one folder), newest first.
func (s *Store) List(dir string, all bool, now time.Time) ([]*Request, error) {
	q := `SELECT ` + cols + ` FROM upload_requests WHERE revoked = 0 AND expires_at > ?`
	args := []any{now.Unix()}
	if !all {
		q += ` AND dir = ?`
		args = append(args, dir)
	}
	rows, err := s.db.Query(q+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Request{}
	for rows.Next() {
		r, _, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Revoke disables a token immediately.
func (s *Store) Revoke(token string) error {
	res, err := s.db.Exec(`UPDATE upload_requests SET revoked = 1 WHERE token = ? AND revoked = 0`, token)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordUpload counts a finished upload.
func (s *Store) RecordUpload(token string, size int64) {
	_, _ = s.db.Exec(`UPDATE upload_requests SET uploads = uploads + 1, bytes = bytes + ?, last_use_at = ? WHERE token = ?`,
		size, time.Now().Unix(), token)
}

// Purge deletes requests expired for more than a day.
func (s *Store) Purge(now time.Time) {
	_, _ = s.db.Exec(`DELETE FROM upload_requests WHERE expires_at < ?`, now.Add(-24*time.Hour).Unix())
}
