// Package session manages browser sessions.
//
// Every session holds its own AES-GCM cipher derived during the ECDH
// handshake. Sessions that are not authenticated yet live only in memory and
// expire quickly. Authenticated sessions are also persisted (see Store) so
// they survive a server restart, and expire at a fixed time after login
// (12 hours by default).
package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"github.com/helviojunior/fmweb/pkg/crypto"
	"github.com/helviojunior/fmweb/pkg/log"
)

// PreAuthTTL is the lifetime of a handshake that was not followed by a login.
const PreAuthTTL = 10 * time.Minute

// Session is one browser session. Cipher is nil for plaintext sessions
// (server started with --no-tls: payload encryption disabled).
type Session struct {
	ID            string
	Cipher        *crypto.Cipher
	Created       time.Time
	mu            sync.Mutex
	expires       time.Time
	authenticated bool
	username      string
	readonly      bool

	persisted bool      // stored in the Store (authenticated)
	checked   time.Time // last time the Store still had it (revocation)

	// multi-step login and MFA enrollment state (memory only)
	login      *LoginState
	mfaPending string
}

// LoginState tracks a login in progress: user -> [MFA] -> password.
type LoginState struct {
	Username string
	Fake     bool // the user does not exist (deception: we pretend it has MFA)
	NeedMFA  bool
	MFAOK    bool
	Expires  time.Time
}

// Login returns a copy of the login state (nil when none / expired).
func (s *Session) Login() *LoginState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.login == nil || time.Now().After(s.login.Expires) {
		return nil
	}
	c := *s.login
	return &c
}

// SetLogin replaces the login state (nil clears it).
func (s *Session) SetLogin(l *LoginState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.login = l
}

// MFAPending returns / sets a TOTP secret being enrolled.
func (s *Session) MFAPending() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mfaPending
}

// SetMFAPending stores a TOTP secret being enrolled ("" clears it).
func (s *Session) SetMFAPending(secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mfaPending = secret
}

// Auth returns the authentication state.
func (s *Session) Auth() (ok bool, user string, readonly bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authenticated, s.username, s.readonly
}

// Expires returns the expiry time.
func (s *Session) Expires() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.expires
}

// SetAuth marks the session as authenticated in memory only (used by --no-auth).
func (s *Session) SetAuth(user string, readonly bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authenticated, s.username, s.readonly = true, user, readonly
}

// SetReadOnly updates the read-only flag (e.g. after the user changed).
func (s *Session) SetReadOnly(ro bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.readonly = ro
}

func (s *Session) expired(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return now.After(s.expires)
}

// Manager keeps sessions in memory, backed by an optional persistent Store.
type Manager struct {
	ttl       time.Duration
	max       int
	plaintext bool
	store     Store

	mu        sync.Mutex
	sessions  map[string]*Session
	lastPurge time.Time
}

// NewManager creates a manager. ttl is the lifetime of an authenticated
// session; store may be nil (memory only); plaintext tells which kind of
// session this server accepts, so persisted sessions of the other kind are
// discarded.
func NewManager(ttl time.Duration, store Store, plaintext bool) *Manager {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	m := &Manager{ttl: ttl, max: 10000, plaintext: plaintext, store: store,
		sessions: map[string]*Session{}, lastPurge: time.Now()}
	m.purge(m.lastPurge)
	return m
}

// TTL returns the lifetime of authenticated sessions.
func (m *Manager) TTL() time.Duration { return m.ttl }

func newID() ([]byte, string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, "", err
	}
	return b, hex.EncodeToString(b), nil
}

// HashID is the identifier stored on disk: the session id itself never is.
func HashID(id string) string {
	sum := sha256.Sum256([]byte("fmweb-session:" + id))
	return hex.EncodeToString(sum[:])
}

// Create runs the handshake and stores a new (not yet authenticated)
// encrypted session.
func (m *Manager) Create(clientPub []byte) (*Session, []byte, error) {
	sidBytes, id, err := newID()
	if err != nil {
		return nil, nil, err
	}
	c, serverPub, err := crypto.Handshake(clientPub, sidBytes)
	if err != nil {
		return nil, nil, err
	}
	return m.add(id, c), serverPub, nil
}

// CreatePlain stores a new session without payload encryption.
func (m *Manager) CreatePlain() (*Session, error) {
	_, id, err := newID()
	if err != nil {
		return nil, err
	}
	return m.add(id, nil), nil
}

func (m *Manager) add(id string, c *crypto.Cipher) *Session {
	now := time.Now()
	s := &Session{ID: id, Cipher: c, Created: now, expires: now.Add(PreAuthTTL)}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(now)
	if len(m.sessions) >= m.max {
		m.evictLocked()
	}
	m.sessions[id] = s
	return s
}

// Authenticate marks the session as logged in, starts its fixed lifetime and
// persists it.
func (m *Manager) Authenticate(s *Session, user string, readonly bool, ip, ua string) error {
	now := time.Now()
	s.mu.Lock()
	s.authenticated, s.username, s.readonly = true, user, readonly
	s.login = nil
	s.expires = now.Add(m.ttl)
	exp := s.expires
	s.mu.Unlock()
	if m.store == nil {
		return nil
	}
	s.mu.Lock()
	s.persisted, s.checked = true, now
	s.mu.Unlock()
	rec := &Record{
		IDHash:    HashID(s.ID),
		Username:  user,
		ReadOnly:  readonly,
		Encrypted: s.Cipher != nil,
		CreatedAt: now,
		ExpiresAt: exp,
		IP:        ip,
		UserAgent: ua,
	}
	if s.Cipher != nil {
		w, err := wrapKey(s.ID, s.Cipher.Key())
		if err != nil {
			return err
		}
		rec.WrappedKey = w
	}
	return m.store.Save(rec)
}

// Get returns a live session, loading it from the store when it is not in
// memory (e.g. after a server restart).
func (m *Manager) Get(id string) *Session {
	if id == "" || len(id) != 32 {
		return nil
	}
	now := time.Now()
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if ok {
		if s.expired(now) || m.revoked(s, now) {
			m.Destroy(id)
			return nil
		}
		return s
	}
	if m.store == nil {
		return nil
	}
	s = m.load(id, now)
	if s == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.sessions[id]; ok {
		return existing
	}
	if len(m.sessions) >= m.max {
		m.evictLocked()
	}
	m.sessions[id] = s
	return s
}

func (m *Manager) load(id string, now time.Time) *Session {
	h := HashID(id)
	rec, err := m.store.Load(h)
	if err != nil {
		log.Debug("session store load failed", "err", err)
		return nil
	}
	if rec == nil {
		return nil
	}
	if now.After(rec.ExpiresAt) || rec.Encrypted == m.plaintext {
		_ = m.store.Delete(h)
		return nil
	}
	s := &Session{ID: id, Created: rec.CreatedAt, expires: rec.ExpiresAt,
		authenticated: true, username: rec.Username, readonly: rec.ReadOnly,
		persisted: true, checked: now}
	if rec.Encrypted {
		key, err := unwrapKey(id, rec.WrappedKey)
		if err != nil {
			_ = m.store.Delete(h)
			return nil
		}
		c, err := crypto.NewCipher(key)
		if err != nil {
			return nil
		}
		s.Cipher = c
	}
	return s
}

// revocationCheck is how often a cached session is checked against the store
// (`fmweb sessions revoke` deletes rows while the server runs).
var revocationCheck = 5 * time.Second

// SetRevocationCheck changes how often cached sessions are re-validated (tests).
func SetRevocationCheck(d time.Duration) { revocationCheck = d }

func (m *Manager) revoked(s *Session, now time.Time) bool {
	if m.store == nil {
		return false
	}
	s.mu.Lock()
	due := s.persisted && now.Sub(s.checked) > revocationCheck
	if due {
		s.checked = now
	}
	s.mu.Unlock()
	if !due {
		return false
	}
	rec, err := m.store.Load(HashID(s.ID))
	return err == nil && rec == nil
}

// Destroy removes a session from memory and from the store.
func (m *Manager) Destroy(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
	if m.store != nil {
		_ = m.store.Delete(HashID(id))
	}
}

// Count returns the number of sessions held in memory.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func (m *Manager) pruneLocked(now time.Time) {
	for id, s := range m.sessions {
		if s.expired(now) {
			delete(m.sessions, id)
		}
	}
	if now.Sub(m.lastPurge) > 10*time.Minute {
		m.lastPurge = now
		go m.purge(now)
	}
}

func (m *Manager) purge(now time.Time) {
	if m.store == nil {
		return
	}
	if n, err := m.store.Purge(now); err != nil {
		log.Debug("session purge failed", "err", err)
	} else if n > 0 {
		log.Debug("expired sessions purged", "count", n)
	}
}

// evictLocked drops the oldest in-memory session (it stays in the store).
func (m *Manager) evictLocked() {
	var oldest *Session
	for _, s := range m.sessions {
		if oldest == nil || s.Created.Before(oldest.Created) {
			oldest = s
		}
	}
	if oldest != nil {
		delete(m.sessions, oldest.ID)
	}
}
