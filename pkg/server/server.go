// Package server is the pure-Go HTTP(S) server: encrypted /api channel plus
// the embedded front-end.
//
//	POST /api/handshake   ECDH handshake (the only plaintext endpoint)
//	POST /api/login       encrypted {username, password}
//	POST /api/logout
//	POST /api/download    encrypted {path} -> encrypted binary frames
//	POST /api/upload      encrypted chunk envelope
//	POST /api/<action>    encrypted JSON action (see package api)
//	GET  /*               embedded front-end (SPA)
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/helviojunior/fmweb/pkg/api"
	"github.com/helviojunior/fmweb/pkg/assets"
	"github.com/helviojunior/fmweb/pkg/auth"
	"github.com/helviojunior/fmweb/pkg/crypto"
	"github.com/helviojunior/fmweb/pkg/fileops"
	"github.com/helviojunior/fmweb/pkg/log"
	"github.com/helviojunior/fmweb/pkg/session"
)

const (
	maxBody       = 32 << 20 // one JSON envelope (upload chunks are ~1.4 MiB)
	downloadChunk = 256 << 10
)

// Config is the server configuration.
type Config struct {
	Addr       string
	TLS        *tls.Certificate // nil = plain http
	NoAuth     bool
	ReadOnly   bool
	Title      string
	SessionTTL time.Duration
	AllowedExt []string
	Users      *auth.Store
	FS         *fileops.FS
}

// Server is the web server.
type Server struct {
	cfg      Config
	sessions *session.Manager
	api      *api.API
	limiter  *loginLimiter
	http     *http.Server
}

// New builds a server.
func New(cfg Config) (*Server, error) {
	if err := assets.Load(); err != nil {
		return nil, fmt.Errorf("embedded front-end: %w", err)
	}
	s := &Server{
		cfg:      cfg,
		sessions: session.NewManager(cfg.SessionTTL, 0),
		api: &api.API{FS: cfg.FS, Opt: api.Options{
			Title: cfg.Title, ReadOnly: cfg.ReadOnly, Auth: !cfg.NoAuth,
		}},
		limiter: newLoginLimiter(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.handleAPI)
	mux.Handle("/", assets.Handler())
	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          nil,
	}
	if cfg.TLS != nil {
		s.http.TLSConfig = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{*cfg.TLS},
		}
	}
	return s, nil
}

// ListenAndServe blocks until ctx is cancelled or the listener fails.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() {
		if s.cfg.TLS != nil {
			errc <- s.http.ServeTLS(ln, "", "")
		} else {
			errc <- s.http.Serve(ln)
		}
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.http.Shutdown(sctx)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; "+
			"connect-src 'self'; font-src 'self' data:; object-src 'none'; "+
			"base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		h.Set("Server", "fmweb")
		next.ServeHTTP(w, r)
	})
}

// ---- helpers -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(status)
	w.Write(b)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"ok": false, "error": msg})
}

func writeSealed(w http.ResponseWriter, sess *session.Session, v any) {
	env, err := sess.Cipher.SealJSON(v)
	if err != nil {
		writeErr(w, 500, "Internal error")
		return
	}
	writeJSON(w, 200, env)
}

type httpErr struct {
	status int
	msg    string
}

func (e *httpErr) Error() string { return e.msg }

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- /api ---------------------------------------------------------------

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "Use POST for /api")
		return
	}
	action := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, "Payload too large")
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			log.Error("panic in api handler", "action", action, "err", rec)
			writeErr(w, 500, "Internal error")
		}
	}()

	if action == "handshake" {
		s.handshake(w, body)
		return
	}
	sess := s.sessions.Get(r.Header.Get("X-Session-Id"))
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "No session")
		return
	}
	switch action {
	case "login":
		s.login(w, r, sess, body)
		return
	case "logout":
		s.sessions.Destroy(sess.ID)
		writeSealed(w, sess, map[string]any{"ok": true})
		return
	}
	authed, user, ro := sess.Auth()
	if !authed {
		writeErr(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	plain, err := s.open(sess, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Decryption failed")
		return
	}
	caller := api.Caller{Username: user, ReadOnly: ro || s.cfg.ReadOnly}
	switch action {
	case "download":
		s.download(w, sess, plain)
		return
	case "upload":
		s.upload(w, sess, plain, caller)
		return
	}
	res, err := s.api.Dispatch(action, plain, caller)
	if err != nil {
		// Application errors travel inside the encrypted body with HTTP 200;
		// status codes are reserved for transport/auth failures.
		var ae *api.Error
		msg := "Operation failed"
		if errors.As(err, &ae) {
			msg = ae.Msg
		}
		writeSealed(w, sess, map[string]any{"ok": false, "error": msg})
		return
	}
	if api.IsWrite(action) {
		log.Info("action", "user", user, "op", action, "ip", clientIP(r))
	}
	writeSealed(w, sess, map[string]any{"ok": true, "result": res})
}

func (s *Server) open(sess *session.Session, body []byte) ([]byte, error) {
	var env crypto.Envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	return sess.Cipher.Open(env)
}

func (s *Server) handshake(w http.ResponseWriter, body []byte) {
	if len(body) > 4096 {
		writeErr(w, 400, "Malformed handshake")
		return
	}
	var req struct {
		ClientPub string `json:"client_pub"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, 400, "Malformed handshake")
		return
	}
	pub, err := crypto.B64.DecodeString(req.ClientPub)
	if err != nil {
		writeErr(w, 400, "Malformed handshake")
		return
	}
	sess, serverPub, err := s.sessions.Create(pub)
	if err != nil {
		writeErr(w, 400, "Handshake failed")
		return
	}
	if s.cfg.NoAuth {
		sess.SetAuth("anonymous", s.cfg.ReadOnly)
	}
	writeJSON(w, 200, map[string]any{
		"session_id":    sess.ID,
		"server_pub":    crypto.B64.EncodeToString(serverPub),
		"auth_required": !s.cfg.NoAuth,
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request, sess *session.Session, body []byte) {
	plain, err := s.open(sess, body)
	if err != nil {
		writeErr(w, 400, "Decryption failed")
		return
	}
	if s.cfg.NoAuth {
		writeSealed(w, sess, map[string]any{"ok": true, "username": "anonymous", "readonly": s.cfg.ReadOnly})
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	_ = json.Unmarshal(plain, &req)
	ip := clientIP(r)
	if wait := s.limiter.blocked(ip); wait > 0 {
		writeSealed(w, sess, map[string]any{"ok": false, "error": "Too many attempts",
			"retry_after": int(wait.Seconds()) + 1})
		return
	}
	u, ok := s.cfg.Users.Verify(req.Username, req.Password)
	if !ok {
		s.limiter.fail(ip)
		log.Warn("login failed", "user", req.Username, "ip", ip)
		writeSealed(w, sess, map[string]any{"ok": false, "error": "Invalid credentials"})
		return
	}
	s.limiter.reset(ip)
	ro := u.ReadOnly || s.cfg.ReadOnly
	sess.SetAuth(u.Username, ro)
	log.Info("login", "user", u.Username, "ip", ip)
	writeSealed(w, sess, map[string]any{"ok": true, "username": u.Username, "readonly": ro})
}

func (s *Server) download(w http.ResponseWriter, sess *session.Session, plain []byte) {
	var req struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(plain, &req)
	fh, st, err := s.cfg.FS.OpenFile(req.Path)
	if err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	defer fh.Close()
	size := st.Size()
	frames := (size + downloadChunk - 1) / downloadChunk
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-File-Size", strconv.FormatInt(size, 10))
	h.Set("Content-Length", strconv.FormatInt(size+frames*32, 10))
	w.WriteHeader(200)
	buf := make([]byte, downloadChunk)
	var sent int64
	for sent < size {
		n, err := io.ReadFull(fh, buf)
		if n > 0 {
			if _, werr := w.Write(sess.Cipher.Frame(buf[:n])); werr != nil {
				return
			}
			sent += int64(n)
		}
		if err != nil {
			return // file shrank: the client detects the short body
		}
	}
}

func (s *Server) upload(w http.ResponseWriter, sess *session.Session, plain []byte, c api.Caller) {
	if c.ReadOnly {
		writeSealed(w, sess, map[string]any{"ok": false, "error": "Read-only mode"})
		return
	}
	var req struct {
		Path   string `json:"path"`
		Name   string `json:"name"`
		Offset int64  `json:"offset"`
		Last   *bool  `json:"last"`
		Chunk  string `json:"chunk"`
	}
	if err := json.Unmarshal(plain, &req); err != nil {
		writeSealed(w, sess, map[string]any{"ok": false, "error": "Malformed request"})
		return
	}
	data, err := crypto.B64.DecodeString(req.Chunk)
	if err != nil {
		writeSealed(w, sess, map[string]any{"ok": false, "error": "Malformed chunk"})
		return
	}
	last := req.Last == nil || *req.Last
	e, received, err := s.cfg.FS.UploadChunk(req.Path, req.Name, req.Offset, data, last, s.cfg.AllowedExt)
	if err != nil {
		writeSealed(w, sess, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if e != nil {
		log.Info("upload", "user", c.Username, "file", e.Path, "size", e.Size)
		writeSealed(w, sess, map[string]any{"ok": true, "result": e})
		return
	}
	writeSealed(w, sess, map[string]any{"ok": true, "result": map[string]any{"received": received}})
}
