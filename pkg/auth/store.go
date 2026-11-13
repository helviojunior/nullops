package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// User is a stored account.
type User struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	ReadOnly     bool      `json:"readonly"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type fileFormat struct {
	Users []*User `json:"users"`
}

// ErrUserExists is returned by Add when the user already exists.
var ErrUserExists = errors.New("user already exists")

var validName = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)

// Store is the JSON-file backed user database.
type Store struct {
	path  string
	mu    sync.RWMutex
	users map[string]*User
	// dummy hash used to equalize timing for unknown users
	dummyOnce sync.Once
	dummy     string
}

// Open loads the user store at path. A missing file yields an empty store.
func Open(path string) (*Store, error) {
	s := &Store{path: path, users: map[string]*User{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, err
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("invalid user database %s: %w", path, err)
	}
	for _, u := range f.Users {
		if u != nil && u.Username != "" {
			s.users[u.Username] = u
		}
	}
	return s, nil
}

// ValidateUsername checks the allowed username charset.
func ValidateUsername(name string) error {
	if !validName.MatchString(name) {
		return errors.New("invalid username (allowed: letters, digits, . _ @ -; max 64)")
	}
	return nil
}

// Count returns the number of users.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users)
}

// Names returns the sorted usernames.
func (s *Store) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.users))
	for n := range s.users {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Get returns a copy of a user.
func (s *Store) Get(name string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[name]
	if !ok {
		return User{}, false
	}
	return *u, true
}

// Set creates or (when update is true) replaces a user's password/flags.
func (s *Store) Set(name, password string, readonly, update bool) error {
	if err := ValidateUsername(name); err != nil {
		return err
	}
	if password == "" {
		return errors.New("empty password")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if u, ok := s.users[name]; ok {
		if !update {
			return ErrUserExists
		}
		u.PasswordHash = hash
		u.ReadOnly = readonly
		u.UpdatedAt = now
	} else {
		s.users[name] = &User{Username: name, PasswordHash: hash,
			ReadOnly: readonly, CreatedAt: now, UpdatedAt: now}
	}
	return s.saveLocked()
}

// Verify checks the credentials and returns the user on success.
func (s *Store) Verify(name, password string) (User, bool) {
	s.mu.RLock()
	u, ok := s.users[name]
	var hash string
	if ok {
		hash = u.PasswordHash
	}
	s.mu.RUnlock()
	if !ok {
		// Spend the same scrypt work to reduce user-enumeration timing signal.
		s.dummyOnce.Do(func() { s.dummy, _ = HashPassword("fmweb-dummy") })
		VerifyPassword(password, s.dummy)
		return User{}, false
	}
	if !VerifyPassword(password, hash) {
		return User{}, false
	}
	return *u, true
}

func (s *Store) saveLocked() error {
	f := fileFormat{Users: make([]*User, 0, len(s.users))}
	for _, n := range sortedKeys(s.users) {
		f.Users = append(f.Users, s.users[n])
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func sortedKeys(m map[string]*User) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
