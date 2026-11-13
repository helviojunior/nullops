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
	MFASecret    string    `json:"mfa_secret,omitempty"`    // base32 TOTP secret; empty = no MFA
	MFALastStep  int64     `json:"mfa_last_step,omitempty"` // last accepted TOTP step (anti-replay)
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// HasMFA reports whether the account requires a TOTP code.
func (u User) HasMFA() bool { return u.MFASecret != "" }

type fileFormat struct {
	Users []*User `json:"users"`
}

// ErrUserExists is returned by Set when the user already exists.
var ErrUserExists = errors.New("user already exists")

// ErrNoUser is returned when the user does not exist.
var ErrNoUser = errors.New("user not found")

var validName = regexp.MustCompile(`^[A-Za-z0-9._@-]{1,64}$`)

// reloadEvery throttles the users.json change check.
const reloadEvery = 2 * time.Second

// Store is the JSON-file backed user database. Changes made to the file by
// another process (e.g. `nullops add-user` while the server runs) are picked up
// automatically.
type Store struct {
	path  string
	mu    sync.RWMutex
	users map[string]*User

	modTime   time.Time
	size      int64
	lastCheck time.Time

	dummyOnce sync.Once
	dummy     string
}

// Open loads the user store at path. A missing file yields an empty store.
func Open(path string) (*Store, error) {
	s := &Store{path: path, users: map[string]*User{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	s.lastCheck = time.Now()
	return s, nil
}

func (s *Store) load() error {
	st, err := os.Stat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		s.users = map[string]*User{}
		s.modTime, s.size = time.Time{}, 0
		return nil
	}
	if err != nil {
		return err
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("invalid user database %s: %w", s.path, err)
	}
	users := map[string]*User{}
	for _, u := range f.Users {
		if u != nil && u.Username != "" {
			users[u.Username] = u
		}
	}
	s.users = users
	s.modTime, s.size = st.ModTime(), st.Size()
	return nil
}

// maybeReload re-reads the file when it changed on disk (throttled).
func (s *Store) maybeReload() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastCheck) < reloadEvery {
		return
	}
	s.lastCheck = time.Now()
	st, err := os.Stat(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if len(s.users) > 0 {
			s.users = map[string]*User{}
			s.modTime, s.size = time.Time{}, 0
		}
		return
	case err != nil:
		return
	}
	if st.ModTime().Equal(s.modTime) && st.Size() == s.size {
		return
	}
	_ = s.load() // keep the previous state on a parse error (partial write)
}

// Reload forces a re-read of the file.
func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastCheck = time.Now()
	return s.load()
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
	s.maybeReload()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users)
}

// Names returns the sorted usernames.
func (s *Store) Names() []string {
	s.maybeReload()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedKeys(s.users)
}

// List returns copies of every user, sorted by name.
func (s *Store) List() []User {
	s.maybeReload()
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0, len(s.users))
	for _, n := range sortedKeys(s.users) {
		out = append(out, *s.users[n])
	}
	return out
}

// Get returns a copy of a user.
func (s *Store) Get(name string) (User, bool) {
	s.maybeReload()
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[name]
	if !ok {
		return User{}, false
	}
	return *u, true
}

// Set creates or (when update is true) replaces a user's password/flags.
// The MFA secret of an existing user is kept.
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
	_ = s.load()
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

// SetReadOnly changes only the read-only flag.
func (s *Store) SetReadOnly(name string, readonly bool) error {
	return s.modify(name, func(u *User) { u.ReadOnly = readonly })
}

// SetMFA enables MFA with secret ("" disables it).
func (s *Store) SetMFA(name, secret string) error {
	return s.modify(name, func(u *User) {
		u.MFASecret = secret
		u.MFALastStep = 0
	})
}

// Delete removes a user.
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.load()
	if _, ok := s.users[name]; !ok {
		return ErrNoUser
	}
	delete(s.users, name)
	return s.saveLocked()
}

func (s *Store) modify(name string, fn func(*User)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.load()
	u, ok := s.users[name]
	if !ok {
		return ErrNoUser
	}
	fn(u)
	u.UpdatedAt = time.Now().UTC()
	return s.saveLocked()
}

// VerifyMFA checks a TOTP code for name, refusing a code that was already used.
func (s *Store) VerifyMFA(name, code string) bool {
	s.maybeReload()
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[name]
	if !ok || u.MFASecret == "" {
		return false
	}
	step, ok := VerifyTOTP(u.MFASecret, code, time.Now())
	if !ok || step <= u.MFALastStep {
		return false
	}
	u.MFALastStep = step
	_ = s.saveLocked()
	return true
}

// Verify checks the password and returns the user on success.
func (s *Store) Verify(name, password string) (User, bool) {
	s.maybeReload()
	s.mu.RLock()
	u, ok := s.users[name]
	var hash string
	if ok {
		hash = u.PasswordHash
	}
	s.mu.RUnlock()
	if !ok {
		// Spend the same scrypt work to reduce user-enumeration timing signal.
		s.dummyOnce.Do(func() { s.dummy, _ = HashPassword("nullops-dummy") })
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
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	if st, err := os.Stat(s.path); err == nil {
		s.modTime, s.size = st.ModTime(), st.Size()
	}
	return nil
}

func sortedKeys(m map[string]*User) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
