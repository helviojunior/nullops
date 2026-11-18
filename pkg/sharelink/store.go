// Package sharelink implements file share links: a signed-in user turns one
// file into a public, opaque URL
//
//	/share/<uuid>?token=<token>
//
// that lets anyone download that single file for a limited time, without an
// account. The <uuid> is the lookup key; the token is the secret capability.
// Both must match an active row or the server answers 404 and reveals nothing.
//
// Unlike the /api channel, the download itself travels as an ordinary HTTP
// response (served by the server package with http.ServeContent), i.e. plain
// bytes with Range support, exactly like a static file behind NGINX.
package sharelink

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
)

const (
	// tokenBytes is the number of random bytes of a token (128 bits), hex-encoded.
	tokenBytes = 16

	// DefaultValidity is used when a caller does not pick a duration.
	DefaultValidity = 24 * time.Hour
	// MinValidity / MaxValidity clamp the requested lifetime.
	MinValidity = 5 * time.Minute
	MaxValidity = 30 * 24 * time.Hour
)

var tokenRe = regexp.MustCompile(`^[a-f0-9]{32}$`)

// ValidToken checks the token format.
func ValidToken(t string) bool { return tokenRe.MatchString(t) }

// ValidID checks that id is a well-formed UUID.
func ValidID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

// ErrNotFound is returned for unknown, revoked, expired or mismatched links.
// Every failure maps to it so the caller cannot tell them apart (-> 404).
var ErrNotFound = errors.New("This share link is invalid or has expired")

// Link is a stored share link.
type Link struct {
	ID        string    `json:"id"`
	Token     string    `json:"token"`
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Downloads int       `json:"downloads"`
	Bytes     int64     `json:"bytes"`
	LastUseAt time.Time `json:"last_used_at,omitempty"`
}

const schema = `
CREATE TABLE IF NOT EXISTS share_links (
	id          TEXT PRIMARY KEY,
	token       TEXT    NOT NULL,
	path        TEXT    NOT NULL,
	name        TEXT    NOT NULL,
	created_by  TEXT    NOT NULL,
	created_at  INTEGER NOT NULL,
	expires_at  INTEGER NOT NULL,
	revoked     INTEGER NOT NULL DEFAULT 0,
	downloads   INTEGER NOT NULL DEFAULT 0,
	bytes       INTEGER NOT NULL DEFAULT 0,
	last_use_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_share_links_path ON share_links(path);
`

// Store keeps share links in SQLite.
type Store struct{ db *sql.DB }

// Open prepares the table.
func Open(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func newToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// clampTTL keeps a requested lifetime inside [MinValidity, MaxValidity];
// a zero or negative value falls back to DefaultValidity.
func clampTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return DefaultValidity
	}
	if ttl < MinValidity {
		return MinValidity
	}
	if ttl > MaxValidity {
		return MaxValidity
	}
	return ttl
}

// Create stores a new link for path (a single file) by user, valid for ttl
// (clamped to [MinValidity, MaxValidity]; 0 = DefaultValidity). name is the
// file's base name, kept for display and the download filename.
func (s *Store) Create(path, name, user string, ttl time.Duration) (*Link, error) {
	now := time.Now()
	ttl = clampTTL(ttl)
	for attempt := 0; attempt < 5; attempt++ {
		tok, err := newToken()
		if err != nil {
			return nil, err
		}
		l := &Link{
			ID: uuid.NewString(), Token: tok, Path: path, Name: name,
			CreatedBy: user, CreatedAt: now, ExpiresAt: now.Add(ttl),
		}
		_, err = s.db.Exec(`INSERT INTO share_links (id, token, path, name, created_by, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, l.ID, l.Token, l.Path, l.Name, l.CreatedBy, now.Unix(), l.ExpiresAt.Unix())
		if err == nil {
			return l, nil
		}
	}
	return nil, errors.New("could not create a unique link")
}

const cols = `id, token, path, name, created_by, created_at, expires_at, revoked, downloads, bytes, last_use_at`

func scan(row interface{ Scan(...any) error }) (*Link, bool, error) {
	var l Link
	var created, expires int64
	var revoked int
	var last sql.NullInt64
	if err := row.Scan(&l.ID, &l.Token, &l.Path, &l.Name, &l.CreatedBy, &created, &expires, &revoked, &l.Downloads, &l.Bytes, &last); err != nil {
		return nil, false, err
	}
	l.CreatedAt, l.ExpiresAt = time.Unix(created, 0), time.Unix(expires, 0)
	if last.Valid {
		l.LastUseAt = time.Unix(last.Int64, 0)
	}
	return &l, revoked != 0, nil
}

// Resolve returns the link addressed by id only when token matches and the
// link is neither revoked nor expired. Any other case returns ErrNotFound, so
// the HTTP layer answers an indistinguishable 404 for a bad id, a bad token,
// a revoked link and an expired one alike. The token is compared in constant
// time.
func (s *Store) Resolve(id, token string, now time.Time) (*Link, error) {
	if !ValidID(id) || !ValidToken(token) {
		return nil, ErrNotFound
	}
	l, revoked, err := scan(s.db.QueryRow(`SELECT `+cols+` FROM share_links WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if revoked || now.After(l.ExpiresAt) {
		return nil, ErrNotFound
	}
	if subtle.ConstantTimeCompare([]byte(l.Token), []byte(token)) != 1 {
		return nil, ErrNotFound
	}
	return l, nil
}

// List returns the active links (optionally only for one file path), newest
// first.
func (s *Store) List(path string, all bool, now time.Time) ([]*Link, error) {
	q := `SELECT ` + cols + ` FROM share_links WHERE revoked = 0 AND expires_at > ?`
	args := []any{now.Unix()}
	if !all {
		q += ` AND path = ?`
		args = append(args, path)
	}
	rows, err := s.db.Query(q+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Link{}
	for rows.Next() {
		l, _, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Revoke disables a link immediately.
func (s *Store) Revoke(id string) error {
	if !ValidID(id) {
		return ErrNotFound
	}
	res, err := s.db.Exec(`UPDATE share_links SET revoked = 1 WHERE id = ? AND revoked = 0`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordDownload counts a finished download.
func (s *Store) RecordDownload(id string, size int64) {
	_, _ = s.db.Exec(`UPDATE share_links SET downloads = downloads + 1, bytes = bytes + ?, last_use_at = ? WHERE id = ?`,
		size, time.Now().Unix(), id)
}

// Purge deletes links expired for more than a day.
func (s *Store) Purge(now time.Time) {
	_, _ = s.db.Exec(`DELETE FROM share_links WHERE expires_at < ?`, now.Add(-24*time.Hour).Unix())
}
