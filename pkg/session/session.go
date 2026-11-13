// Package session is the in-memory session store. Every session holds its
// own AES-GCM cipher derived during the ECDH handshake; nothing is persisted.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/helviojunior/fmweb/pkg/crypto"
)

// Session is one browser session.
type Session struct {
	ID            string
	Cipher        *crypto.Cipher
	Created       time.Time
	mu            sync.Mutex
	lastSeen      time.Time
	authenticated bool
	username      string
	readonly      bool
}

// Auth returns the authentication state.
func (s *Session) Auth() (ok bool, user string, readonly bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authenticated, s.username, s.readonly
}

// SetAuth marks the session as authenticated.
func (s *Session) SetAuth(user string, readonly bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.authenticated, s.username, s.readonly = true, user, readonly
}

// Manager stores sessions with an idle TTL.
type Manager struct {
	ttl      time.Duration
	max      int
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager creates a manager. max caps live sessions (DoS guard).
func NewManager(ttl time.Duration, max int) *Manager {
	if max <= 0 {
		max = 10000
	}
	return &Manager{ttl: ttl, max: max, sessions: map[string]*Session{}}
}

// Create runs the handshake and stores a new session.
func (m *Manager) Create(clientPub []byte) (*Session, []byte, error) {
	sidBytes := make([]byte, 16)
	if _, err := rand.Read(sidBytes); err != nil {
		return nil, nil, err
	}
	c, serverPub, err := crypto.Handshake(clientPub, sidBytes)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	s := &Session{ID: hex.EncodeToString(sidBytes), Cipher: c, Created: now, lastSeen: now}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(now)
	if len(m.sessions) >= m.max {
		m.evictOldestLocked()
	}
	m.sessions[s.ID] = s
	return s, serverPub, nil
}

// Get returns a live session and refreshes its idle timer.
func (m *Manager) Get(id string) *Session {
	if id == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil
	}
	now := time.Now()
	if now.Sub(s.lastSeen) > m.ttl {
		delete(m.sessions, id)
		return nil
	}
	s.lastSeen = now
	return s
}

// Destroy removes a session.
func (m *Manager) Destroy(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// Count returns the number of live sessions.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func (m *Manager) pruneLocked(now time.Time) {
	for id, s := range m.sessions {
		if now.Sub(s.lastSeen) > m.ttl {
			delete(m.sessions, id)
		}
	}
}

func (m *Manager) evictOldestLocked() {
	var oldest *Session
	for _, s := range m.sessions {
		if oldest == nil || s.lastSeen.Before(oldest.lastSeen) {
			oldest = s
		}
	}
	if oldest != nil {
		delete(m.sessions, oldest.ID)
	}
}
