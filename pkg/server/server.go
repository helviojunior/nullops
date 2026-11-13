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
	Addr string
	TLS  *tls.Certificate // nil = plain http
	// Plaintext disables the payload encryption of /api (no WebCrypto). Only
	// used with --no-tls, after the operator explicitly confirmed it.
	Plaintext  bool
	NoAuth     bool
	ReadOnly   bool
	Title      string
	SessionTTL time.Duration
	Sessions   session.Store // persistent session store (nil = memory only)
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
		sessions: session.NewManager(cfg.SessionTTL, cfg.Sessions, cfg.Plaintext),
		api: &api.API{FS: cfg.FS, Opt: api.Options{
			Title: cfg.Title, ReadOnly: cfg.ReadOnly, Auth: !cfg.NoAuth,
		}},
		limiter: newLoginLimiter(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.handleAPI)
	mux.Handle("/", logAccess(assets.Handler()))
	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           traceRequests(securityHeaders(mux)),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
		// TLS handshake errors ("bad certificate" from browsers that do not
		// trust the self-signed cert yet) and connection noise: debug only.
		ErrorLog: log.StdDebugLogger("http server"),
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
	if sess.Cipher == nil {
		writeJSON(w, 200, v)
		return
	}
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
		if ok, u, _ := sess.Auth(); ok {
			log.Info("logout", "user", u, "ip", clientIP(r))
		}
		s.sessions.Destroy(sess.ID)
		writeSealed(w, sess, map[string]any{"ok": true})
		return
	}
	authed, user, ro := sess.Auth()
	if !authed {
		writeErr(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	if !s.cfg.NoAuth {
		// A persisted session outlives restarts: re-check the account.
		u, ok := s.cfg.Users.Get(user)
		if !ok {
			s.sessions.Destroy(sess.ID)
			writeErr(w, http.StatusUnauthorized, "Not authenticated")
			return
		}
		if u.ReadOnly != ro && !s.cfg.ReadOnly {
			ro = u.ReadOnly
			sess.SetReadOnly(ro)
		}
	}
	plain, err := s.open(sess, body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Decryption failed")
		return
	}
	caller := api.Caller{Username: user, ReadOnly: ro || s.cfg.ReadOnly}
	switch action {
	case "download":
		s.download(w, r, sess, plain, caller)
		return
	case "upload":
		s.upload(w, r, sess, plain, caller)
		return
	}
	res, err := s.api.Dispatch(action, plain, caller)
	auditAction(r, user, action, plain, err)
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
	writeSealed(w, sess, map[string]any{"ok": true, "result": res})
}

func (s *Server) open(sess *session.Session, body []byte) ([]byte, error) {
	if sess.Cipher == nil {
		if len(body) > 0 && !json.Valid(body) {
			return nil, errors.New("invalid json")
		}
		return body, nil
	}
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
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeErr(w, 400, "Malformed handshake")
			return
		}
	}

	var sess *session.Session
	resp := map[string]any{"auth_required": !s.cfg.NoAuth}
	if s.cfg.Plaintext {
		var err error
		if sess, err = s.sessions.CreatePlain(); err != nil {
			writeErr(w, 500, "Handshake failed")
			return
		}
		resp["encrypted"] = false
	} else {
		if req.ClientPub == "" {
			// The browser has no WebCrypto (not a secure context).
			writeJSON(w, 400, map[string]any{"ok": false, "error": "Secure context required",
				"code": "secure_context", "encrypted": true})
			return
		}
		pub, err := crypto.B64.DecodeString(req.ClientPub)
		if err != nil {
			writeErr(w, 400, "Malformed handshake")
			return
		}
		var serverPub []byte
		sess, serverPub, err = s.sessions.Create(pub)
		if err != nil {
			writeErr(w, 400, "Handshake failed")
			return
		}
		resp["encrypted"] = true
		resp["server_pub"] = crypto.B64.EncodeToString(serverPub)
	}
	if s.cfg.NoAuth {
		sess.SetAuth("anonymous", s.cfg.ReadOnly)
	}
	resp["session_id"] = sess.ID
	writeJSON(w, 200, resp)
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
	ip, ua := clientIP(r), userAgent(r)
	if wait := s.limiter.blocked(ip); wait > 0 {
		log.Warn("login blocked (too many attempts)", "user", req.Username, "ip", ip, "ua", ua)
		writeSealed(w, sess, map[string]any{"ok": false, "error": "Too many attempts",
			"retry_after": int(wait.Seconds()) + 1})
		return
	}
	u, ok := s.cfg.Users.Verify(req.Username, req.Password)
	if !ok {
		s.limiter.fail(ip)
		log.Warn("login failed", "user", req.Username, "ip", ip, "ua", ua)
		writeSealed(w, sess, map[string]any{"ok": false, "error": "Invalid credentials"})
		return
	}
	s.limiter.reset(ip)
	ro := u.ReadOnly || s.cfg.ReadOnly
	if err := s.sessions.Authenticate(sess, u.Username, ro, ip, ua); err != nil {
		log.Error("cannot persist session", "err", err)
	}
	log.Info("login", "user", u.Username, "ip", ip, "ua", ua, "readonly", ro)
	writeSealed(w, sess, map[string]any{"ok": true, "username": u.Username, "readonly": ro,
		"expires_at": sess.Expires().Unix()})
}

func (s *Server) download(w http.ResponseWriter, r *http.Request, sess *session.Session, plain []byte, c api.Caller) {
	var req struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(plain, &req)
	fh, st, err := s.cfg.FS.OpenFile(req.Path)
	if err != nil {
		log.Warn("download failed", "user", c.Username, "ip", clientIP(r), "path", "/"+req.Path, "err", err.Error())
		writeErr(w, 404, err.Error())
		return
	}
	defer fh.Close()
	log.Info("download", "user", c.Username, "ip", clientIP(r), "path", "/"+req.Path, "size", st.Size())
	size := st.Size()
	frames := (size + downloadChunk - 1) / downloadChunk
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-File-Size", strconv.FormatInt(size, 10))
	if sess.Cipher == nil {
		h.Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(200)
		io.CopyN(w, fh, size)
		return
	}
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

func (s *Server) upload(w http.ResponseWriter, r *http.Request, sess *session.Session, plain []byte, c api.Caller) {
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
		log.Warn("upload failed", "user", c.Username, "ip", clientIP(r), "path", "/"+joinRel(req.Path, req.Name), "err", err.Error())
		writeSealed(w, sess, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if e != nil {
		log.Info("upload", "user", c.Username, "ip", clientIP(r), "path", "/"+e.Path, "size", e.Size)
		writeSealed(w, sess, map[string]any{"ok": true, "result": e})
		return
	}
	writeSealed(w, sess, map[string]any{"ok": true, "result": map[string]any{"received": received}})
}
