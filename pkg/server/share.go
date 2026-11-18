package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/helviojunior/nullops/pkg/sharelink"
)

// shareIDHint shortens an id for the audit log (never log the token).
func shareIDHint(id string) string {
	if len(id) < 8 {
		return "?"
	}
	return id[:8] + "…"
}

// shareAction handles the signed-in side of share links:
//
//	share_create {path, ttl}   -> {id, token, url, expires_at, path, name}
//	share_list   {path, all}   -> [links]
//	share_revoke {id}          -> {}
//
// ttl is the requested validity in seconds (clamped by the store; 0 = default).
func (s *Server) shareAction(r *http.Request, user string, readonly bool, action string, plain []byte) (any, error) {
	st := s.cfg.ShareLinks
	if st == nil {
		return nil, errors.New("Share links are not available")
	}
	var req struct {
		Path string `json:"path"`
		All  bool   `json:"all"`
		ID   string `json:"id"`
		TTL  int64  `json:"ttl"` // seconds
	}
	_ = json.Unmarshal(plain, &req)
	ip := s.clientIP(r)
	switch action {
	case "share_list":
		return st.List(req.Path, req.All, time.Now())
	case "share_create":
		if readonly || s.cfg.ReadOnly {
			return nil, errors.New("Read-only mode")
		}
		e, err := s.cfg.FS.Stat(req.Path)
		if err != nil {
			return nil, errors.New("File not found")
		}
		if e.IsDir {
			return nil, errors.New("Only files can be shared")
		}
		l, err := st.Create(e.Path, e.Name, user, time.Duration(req.TTL)*time.Second)
		if err != nil {
			return nil, err
		}
		auditInfo("share link created", "user", user, "ip", ip, "path", "/"+e.Path,
			"id", shareIDHint(l.ID), "expires", l.ExpiresAt.Format(time.RFC3339))
		return map[string]any{"id": l.ID, "token": l.Token, "url": shareURL(l.ID, l.Token),
			"expires_at": l.ExpiresAt, "path": e.Path, "name": e.Name}, nil
	case "share_revoke":
		if readonly || s.cfg.ReadOnly {
			return nil, errors.New("Read-only mode")
		}
		if err := st.Revoke(req.ID); err != nil {
			return nil, err
		}
		auditInfo("share link revoked", "user", user, "ip", ip, "id", shareIDHint(req.ID))
		return map[string]any{}, nil
	}
	return nil, errors.New("Unknown action: " + action)
}

// shareURL builds the opaque, relative download URL.
func shareURL(id, token string) string {
	return "/share/" + id + "?token=" + url.QueryEscape(token)
}

// handleShare serves a public, cleartext file download:
//
//	GET /share/<uuid>?token=<token>
//
// It verifies the UUID and the token; if either is missing, malformed, unknown,
// revoked or expired it answers a plain 404 that reveals nothing. On success it
// streams the file as an ordinary HTTP response (http.ServeContent: correct
// Content-Type, Content-Length, Last-Modified and Range support), i.e. exactly
// like downloading a conventional file from a static server such as NGINX — it
// does NOT go through the encrypted /api channel.
func (s *Server) handleShare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	st := s.cfg.ShareLinks
	if st == nil {
		http.NotFound(w, r)
		return
	}

	ip, ua := s.clientIP(r), userAgent(r)
	if wait := s.limiter.blocked(ip); wait > 0 {
		w.Header().Set("Retry-After", retryAfter(wait))
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}

	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/share/"), "/")
	token := r.URL.Query().Get("token")

	l, err := st.Resolve(id, token, time.Now())
	if err != nil {
		// A malformed/empty id or token is a stray probe, not an attack; only
		// a well-formed pair that still fails to resolve counts towards the
		// per-IP lockout.
		if sharelink.ValidID(id) && sharelink.ValidToken(token) {
			s.limiter.fail(ip)
			auditWarn("share download: invalid link", "ip", ip, "ua", ua, "id", shareIDHint(id))
		}
		http.NotFound(w, r)
		return
	}

	fh, fi, ferr := s.cfg.FS.OpenFile(l.Path)
	if ferr != nil {
		// The file was moved or deleted after the link was made: 404, same as
		// an unknown link.
		auditWarn("share download: missing file", "ip", ip, "id", shareIDHint(l.ID), "path", "/"+l.Path, "err", ferr.Error())
		http.NotFound(w, r)
		return
	}
	defer fh.Close()

	name := l.Name
	if name == "" {
		name = path.Base(l.Path)
	}
	h := w.Header()
	h.Set("Content-Disposition", contentDisposition(name))
	h.Set("X-Content-Type-Options", "nosniff")
	// A shared link is a capability URL: keep it out of shared/proxy caches.
	h.Set("Cache-Control", "private, no-store")

	auditInfo("share download", "ip", ip, "ua", ua, "id", shareIDHint(l.ID), "path", "/"+l.Path, "size", fi.Size())
	// ServeContent handles HEAD, Range, If-Modified-Since and Content-Length.
	http.ServeContent(w, r, name, fi.ModTime(), fh)
	if r.Method == http.MethodGet {
		st.RecordDownload(l.ID, fi.Size())
	}
}

// contentDisposition builds an "attachment" header (RFC 6266). It always emits
// a quoted ASCII filename, and only adds the RFC 5987 filename* form when the
// name actually has non-ASCII bytes — a plain ASCII name like "README.md" needs
// no extended form, so it is not duplicated.
func contentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	if ascii == "" {
		ascii = "download"
	}
	cd := `attachment; filename="` + ascii + `"`
	if isASCII(name) {
		return cd
	}
	return cd + `; filename*=UTF-8''` + rfc5987Escape(name)
}

// isASCII reports whether s contains only printable ASCII (0x20–0x7e).
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// rfc5987Escape percent-encodes s for the RFC 5987 ext-value grammar: every
// byte that is not an attr-char is written as %XX (over the UTF-8 bytes, which
// is what "UTF-8”" declares). Unlike url.PathEscape this also escapes '=',
// '@', ',', ';' and space, which are not attr-chars.
func rfc5987Escape(s string) string {
	const upperhex = "0123456789ABCDEF"
	const attrChar = "!#$&+-.^_`|~" // plus ALPHA / DIGIT, handled below
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			strings.IndexByte(attrChar, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(upperhex[c>>4])
		b.WriteByte(upperhex[c&0x0f])
	}
	return b.String()
}

// retryAfter renders a lockout duration as whole seconds for Retry-After.
func retryAfter(d time.Duration) string {
	secs := int(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return strconv.Itoa(secs)
}
