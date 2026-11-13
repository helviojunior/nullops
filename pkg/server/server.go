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

	"github.com/helviojunior/nullops/pkg/api"
	"github.com/helviojunior/nullops/pkg/assets"
	"github.com/helviojunior/nullops/pkg/auth"
	"github.com/helviojunior/nullops/pkg/crypto"
	"github.com/helviojunior/nullops/pkg/fileops"
	"github.com/helviojunior/nullops/pkg/log"
	"github.com/helviojunior/nullops/pkg/pads"
	"github.com/helviojunior/nullops/pkg/session"
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
	// TrustedProxies whose X-Forwarded-For / X-Real-IP are honoured.
	TrustedProxies []*net.IPNet
	// MaxUpload caps the size of one uploaded file in bytes (0 = unlimited).
	MaxUpload  int64
	AllowedExt []string
	Users      *auth.Store
	FS         *fileops.FS
	// Pads is the PastePad manager (nil = disabled).
	Pads *pads.Manager
}

// Server is the web server.
type Server struct {
	cfg      Config
	sessions *session.Manager
	api      *api.API
	limiter  *loginLimiter
	http     *http.Server

	// every request context derives from base; cancelling it on shutdown
	// ends the PastePad long polls so the server stops right away
	base       context.Context
	cancelBase context.CancelFunc
}

// New builds a server.
func New(cfg Config) (*Server, error) {
	if err := assets.Load(); err != nil {
		return nil, fmt.Errorf("embedded front-end: %w", err)
	}
	return newServer(cfg), nil
}

// newServer builds the server without checking the embedded front-end
// (unit tests exercise the API only).
func newServer(cfg Config) *Server {
	s := &Server{
		cfg:      cfg,
		sessions: session.NewManager(cfg.SessionTTL, cfg.Sessions, cfg.Plaintext),
		api: &api.API{FS: cfg.FS, Opt: api.Options{
			Title: cfg.Title, ReadOnly: cfg.ReadOnly, Auth: !cfg.NoAuth,
		}},
		limiter: newLoginLimiter(),
	}
	s.base, s.cancelBase = context.WithCancel(context.Background())
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", s.handleAPI)
	mux.Handle("/", s.logAccess(assets.Handler()))
	s.http = &http.Server{
		Addr:              cfg.Addr,
		Handler:           s.traceRequests(securityHeaders(mux)),
		ReadHeaderTimeout: 15 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return s.base },
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
	return s
}

// ListenAndServe blocks until ctx is cancelled or the listener fails.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Addr)
	if err != nil {
		return err
	}
	return s.Serve(ctx, ln)
}

// Serve serves on ln until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
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
		// wake up long polls first, then stop accepting and drain
		s.cancelBase()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.http.Shutdown(sctx); err != nil {
			// a request still running (e.g. a large download) after the grace
			// period: close the remaining connections instead of failing
			log.Debug("forcing shutdown", "err", err)
			_ = s.http.Close()
		}
		return nil
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
			// blob: frames: PDF preview of a decrypted download
			"frame-src 'self' blob:; "+
			"base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		h.Set("Server", "nullops")
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
	case "login_start", "login_mfa", "login":
		s.loginStep(w, r, sess, action, body)
		return
	case "logout":
		if ok, u, _ := sess.Auth(); ok {
			auditInfo("logout", "user", u, "ip", s.clientIP(r))
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
	if strings.HasPrefix(action, "pad_") {
		s.padAction(w, r, sess, user, action, plain)
		return
	}
	if strings.HasPrefix(action, "mfa_") {
		res, msg := s.accountAction(r, sess, user, action, plain)
		if msg != "" {
			writeSealed(w, sess, map[string]any{"ok": false, "error": msg})
			return
		}
		writeSealed(w, sess, map[string]any{"ok": true, "result": res})
		return
	}
	res, err := s.api.Dispatch(action, plain, caller)
	s.auditAction(r, user, action, plain, err)
	if err != nil {
		// Application errors travel inside the encrypted body with HTTP 200;
		// status codes are reserved for transport/auth failures.
		var ae *api.Error
		out := map[string]any{"ok": false, "error": "Operation failed"}
		if errors.As(err, &ae) {
			out["error"] = ae.Msg
			if ae.Code != "" {
				out["code"] = ae.Code
			}
		}
		writeSealed(w, sess, out)
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

func (s *Server) download(w http.ResponseWriter, r *http.Request, sess *session.Session, plain []byte, c api.Caller) {
	var req struct {
		Path  string   `json:"path"`
		Paths []string `json:"paths"`
	}
	_ = json.Unmarshal(plain, &req)
	if len(req.Paths) > 0 {
		s.downloadZip(w, r, sess, req.Paths, c)
		return
	}
	if st, err := s.cfg.FS.Stat(req.Path); err == nil && st.IsDir {
		s.downloadZip(w, r, sess, []string{req.Path}, c)
		return
	}
	fh, st, err := s.cfg.FS.OpenFile(req.Path)
	if err != nil {
		auditWarn("download failed", "user", c.Username, "ip", s.clientIP(r), "path", "/"+req.Path, "err", err.Error())
		writeErr(w, 404, err.Error())
		return
	}
	defer fh.Close()
	auditInfo("download", "user", c.Username, "ip", s.clientIP(r), "path", "/"+req.Path, "size", st.Size())
	size := st.Size()
	frames := (size + downloadChunk - 1) / downloadChunk
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-File-Size", strconv.FormatInt(size, 10))
	if sess.Cipher == nil {
		h.Set("Content-Length", strconv.FormatInt(size, 10))
		w.WriteHeader(200)
		if n, err := io.CopyN(w, fh, size); err != nil && r.Context().Err() != nil {
			auditInfo("download canceled", "user", c.Username, "ip", s.clientIP(r), "path", "/"+req.Path,
				"sent", n, "size", size)
		}
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
				// the browser canceled the transfer (or the connection dropped)
				auditInfo("download canceled", "user", c.Username, "ip", s.clientIP(r), "path", "/"+req.Path,
					"sent", sent, "size", size)
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
		ID     string `json:"upload_id"`
		Offset int64  `json:"offset"`
		Total  *int64 `json:"size"`
		Last   *bool  `json:"last"`
		Chunk  string `json:"chunk"`
		Status bool   `json:"status"` // only ask how much is already uploaded
	}
	if err := json.Unmarshal(plain, &req); err != nil {
		writeSealed(w, sess, map[string]any{"ok": false, "error": "Malformed request"})
		return
	}
	total := int64(-1)
	if req.Total != nil {
		total = *req.Total
	}
	if req.Status {
		if s.cfg.MaxUpload > 0 && total > s.cfg.MaxUpload {
			writeSealed(w, sess, map[string]any{"ok": false, "error": "File too large", "code": "too_large"})
			return
		}
		n, err := s.cfg.FS.UploadStatus(req.Path, req.Name, req.ID)
		if err != nil {
			writeSealed(w, sess, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if total >= 0 && n > total {
			n = 0
		}
		writeSealed(w, sess, map[string]any{"ok": true, "result": map[string]any{"received": n, "max_size": s.cfg.MaxUpload}})
		return
	}
	data, err := crypto.B64.DecodeString(req.Chunk)
	if err != nil {
		writeSealed(w, sess, map[string]any{"ok": false, "error": "Malformed chunk"})
		return
	}
	last := req.Last == nil || *req.Last
	e, received, err := s.cfg.FS.UploadChunk(fileops.UploadRequest{
		Dir: req.Path, Name: req.Name, ID: req.ID, Offset: req.Offset, Total: total,
		Data: data, Last: last, MaxSize: s.cfg.MaxUpload, AllowExt: s.cfg.AllowedExt,
	})
	if err != nil {
		auditWarn("upload failed", "user", c.Username, "ip", s.clientIP(r), "path", "/"+joinRel(req.Path, req.Name), "err", err.Error())
		writeSealed(w, sess, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if e != nil {
		auditInfo("upload", "user", c.Username, "ip", s.clientIP(r), "path", "/"+e.Path, "size", e.Size)
		writeSealed(w, sess, map[string]any{"ok": true, "result": e})
		return
	}
	writeSealed(w, sess, map[string]any{"ok": true, "result": map[string]any{"received": received}})
}

// frameWriter buffers a stream and emits it as encrypted frames (or raw bytes
// for plaintext sessions).
type frameWriter struct {
	w   http.ResponseWriter
	c   *crypto.Cipher
	buf []byte
	err error
}

func (f *frameWriter) Write(p []byte) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	n := len(p)
	for len(p) > 0 {
		room := downloadChunk - len(f.buf)
		if room > len(p) {
			room = len(p)
		}
		f.buf = append(f.buf, p[:room]...)
		p = p[room:]
		if len(f.buf) == downloadChunk {
			f.flush()
		}
	}
	return n, f.err
}

func (f *frameWriter) flush() {
	if len(f.buf) == 0 || f.err != nil {
		return
	}
	out := f.buf
	if f.c != nil {
		out = f.c.Frame(f.buf)
	}
	if _, err := f.w.Write(out); err != nil {
		f.err = err
	}
	f.buf = f.buf[:0]
}

// downloadZip streams a zip of folders / a selection, built on the fly.
func (s *Server) downloadZip(w http.ResponseWriter, r *http.Request, sess *session.Session, paths []string, c api.Caller) {
	for _, p := range paths {
		if _, err := s.cfg.FS.Stat(p); err != nil {
			auditWarn("download failed", "user", c.Username, "ip", s.clientIP(r), "paths", strings.Join(paths, ", "), "err", err.Error())
			writeErr(w, 404, err.Error())
			return
		}
	}
	auditInfo("download zip", "user", c.Username, "ip", s.clientIP(r), "paths", "/"+strings.Join(paths, ", /"))
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Archive", "zip")
	w.WriteHeader(200)
	fw := &frameWriter{w: w, c: sess.Cipher, buf: make([]byte, 0, downloadChunk)}
	if err := s.cfg.FS.ZipTo(fw, paths); err != nil {
		if fw.err != nil {
			auditInfo("download canceled", "user", c.Username, "ip", s.clientIP(r), "paths", "/"+strings.Join(paths, ", /"))
		} else {
			log.Debug("zip stream aborted", "err", err)
		}
		return // the client sees a truncated stream and reports the failure
	}
	fw.flush()
}

// Handler exposes the HTTP handler (tests).
func (s *Server) Handler() http.Handler { return s.http.Handler }
