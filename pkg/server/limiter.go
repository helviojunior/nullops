package server

import (
	"sync"
	"time"
)

// loginLimiter throttles brute-force attempts per client IP: after maxFails
// failures inside window, the IP is locked out for lockout.
type loginLimiter struct {
	mu    sync.Mutex
	state map[string]*limitState
}

type limitState struct {
	fails int
	first time.Time
	until time.Time
}

const (
	maxFails = 5
	window   = 5 * time.Minute
	lockout  = 5 * time.Minute
)

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{state: map[string]*limitState{}}
}

func (l *loginLimiter) blocked(ip string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.state[ip]
	if !ok {
		return 0
	}
	if d := time.Until(st.until); d > 0 {
		return d
	}
	return 0
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.state) > 10000 {
		for k, v := range l.state {
			if now.Sub(v.first) > window && now.After(v.until) {
				delete(l.state, k)
			}
		}
	}
	st, ok := l.state[ip]
	if !ok || now.Sub(st.first) > window {
		st = &limitState{first: now}
		l.state[ip] = st
	}
	st.fails++
	if st.fails >= maxFails {
		st.until = now.Add(lockout)
		st.fails = 0
		st.first = now
	}
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.state, ip)
}
