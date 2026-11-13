package pads

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Meta describes a pad.
type Meta struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
	Size      int       `json:"size"`
	key       []byte
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// ValidName checks the allowed pad names: [A-Za-z0-9._-], 1-64, not "." or "..".
func ValidName(name string) bool {
	return nameRe.MatchString(name) && name != "." && name != ".."
}

var (
	// ErrNotFound is returned for an unknown pad.
	ErrNotFound = errors.New("Pad not found")
	// ErrExists is returned when creating a pad whose name is taken.
	ErrExists = errors.New("A pad with this name already exists")
	// ErrInvalidName is returned for names outside [A-Za-z0-9._-].
	ErrInvalidName = errors.New("Invalid pad name (allowed: letters, digits, . _ -)")
)

const schema = `
CREATE TABLE IF NOT EXISTS pads (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL UNIQUE,
	enc_key    BLOB NOT NULL,
	created_by TEXT,
	created_at INTEGER NOT NULL,
	updated_by TEXT,
	updated_at INTEGER NOT NULL,
	size       INTEGER NOT NULL DEFAULT 0
);
`

// Store keeps pad metadata and AES-256 keys in SQLite and the content in
// <dir>/<uuid>, encrypted with AES-256-GCM (nonce | ciphertext, AAD = uuid).
type Store struct {
	db  *sql.DB
	dir string
}

// OpenStore prepares the pads table and directory.
func OpenStore(db *sql.DB, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db, dir: dir}, nil
}

func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]), nil
}

const cols = `id, name, enc_key, created_by, created_at, updated_by, updated_at, size`

func scan(row interface{ Scan(...any) error }) (*Meta, error) {
	var m Meta
	var cb, ub sql.NullString
	var ca, ua int64
	if err := row.Scan(&m.ID, &m.Name, &m.key, &cb, &ca, &ub, &ua, &m.Size); err != nil {
		return nil, err
	}
	m.CreatedBy, m.UpdatedBy = cb.String, ub.String
	m.CreatedAt, m.UpdatedAt = time.Unix(ca, 0), time.Unix(ua, 0)
	return &m, nil
}

// List returns every pad, most recently updated first.
func (s *Store) List() ([]*Meta, error) {
	rows, err := s.db.Query(`SELECT ` + cols + ` FROM pads ORDER BY updated_at DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Meta
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Get returns a pad by name.
func (s *Store) Get(name string) (*Meta, error) {
	m, err := scan(s.db.QueryRow(`SELECT `+cols+` FROM pads WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

// Create registers a new empty pad with a fresh random key.
func (s *Store) Create(name, user string) (*Meta, error) {
	if !ValidName(name) {
		return nil, ErrInvalidName
	}
	if _, err := s.Get(name); err == nil {
		return nil, ErrExists
	}
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	now := time.Now()
	m := &Meta{ID: id, Name: name, CreatedBy: user, CreatedAt: now, UpdatedBy: user, UpdatedAt: now, key: key}
	if _, err := s.db.Exec(`INSERT INTO pads (`+cols+`) VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
		id, name, key, user, now.Unix(), user, now.Unix()); err != nil {
		return nil, ErrExists
	}
	if err := s.SaveContent(m, nil, 0); err != nil {
		return nil, err
	}
	return m, nil
}

// Delete removes a pad and its encrypted file.
func (s *Store) Delete(name string) error {
	m, err := s.Get(name)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM pads WHERE id = ?`, m.ID); err != nil {
		return err
	}
	_ = os.Remove(s.file(m.ID))
	return nil
}

func (s *Store) file(id string) string { return filepath.Join(s.dir, id) }

type content struct {
	Rev  int    `json:"rev"`
	Text string `json:"text"`
}

func aead(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// LoadContent decrypts the pad file.
func (s *Store) LoadContent(m *Meta) (Text, int, error) {
	raw, err := os.ReadFile(s.file(m.ID))
	if errors.Is(err, os.ErrNotExist) {
		return Text{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	a, err := aead(m.key)
	if err != nil {
		return nil, 0, err
	}
	if len(raw) < a.NonceSize() {
		return nil, 0, errors.New("pad file truncated")
	}
	plain, err := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], []byte(m.ID))
	if err != nil {
		return nil, 0, fmt.Errorf("pad %s cannot be decrypted", m.Name)
	}
	var c content
	if err := json.Unmarshal(plain, &c); err != nil {
		return nil, 0, err
	}
	return FromString(c.Text), c.Rev, nil
}

// SaveContent encrypts and atomically writes the pad file, then updates the
// metadata row.
func (s *Store) SaveContent(m *Meta, text Text, rev int) error {
	plain, err := json.Marshal(content{Rev: rev, Text: text.String()})
	if err != nil {
		return err
	}
	a, err := aead(m.key)
	if err != nil {
		return err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	out := a.Seal(nonce, nonce, plain, []byte(m.ID))
	tmp := s.file(m.ID) + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.file(m.ID)); err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE pads SET updated_by = ?, updated_at = ?, size = ? WHERE id = ?`,
		m.UpdatedBy, m.UpdatedAt.Unix(), len(text), m.ID)
	return err
}
