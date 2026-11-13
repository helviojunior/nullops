package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/helviojunior/fmweb/pkg/api"
	"github.com/helviojunior/fmweb/pkg/assets"
	"github.com/helviojunior/fmweb/pkg/log"
)

// Console audit log: page access, authentication and file operations.

func userAgent(r *http.Request) string {
	ua := strings.TrimSpace(r.UserAgent())
	if ua == "" {
		return "-"
	}
	if len(ua) > 200 {
		ua = ua[:200] + "…"
	}
	return ua
}

// logAccess logs page loads (GET / and SPA routes), not every static asset.
func (s *Server) logAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && assets.IsPageRequest(r.URL.Path) {
			auditInfo("access", "ip", s.clientIP(r), "path", r.URL.Path, "ua", userAgent(r))
		}
		next.ServeHTTP(w, r)
	})
}

// opNames maps API actions to the verb shown in the log.
var opNames = map[string]string{
	"save": "edit", "mkdir": "mkdir", "touch": "create", "rename": "rename",
	"delete": "delete", "copy": "copy", "move": "move", "chmod": "chmod",
	"zip": "zip", "tar": "tar", "extract": "extract", "read": "open",
}

// auditAction logs a file operation with its relevant arguments.
func (s *Server) auditAction(r *http.Request, user, action string, plain []byte, err error) {
	op, ok := opNames[action]
	if !ok {
		// list/stat/search/info: debug only (-v)
		if log.Verbosity() >= 1 {
			var p api.Payload
			_ = json.Unmarshal(plain, &p)
			kv := []any{"user", user, "ip", s.clientIP(r), "path", "/" + p.Path}
			if p.Query != "" {
				kv = append(kv, "query", p.Query)
			}
			if err != nil {
				kv = append(kv, "err", err.Error())
			}
			log.Debug(action, kv...)
		}
		return
	}
	var p api.Payload
	_ = json.Unmarshal(plain, &p)
	kv := []any{"user", user, "ip", s.clientIP(r)}
	switch action {
	case "mkdir", "touch":
		kv = append(kv, "path", "/"+joinRel(p.Path, p.Name))
	case "rename":
		kv = append(kv, "path", "/"+p.Path, "to", p.Name)
	case "delete":
		kv = append(kv, "paths", listPaths(p))
	case "copy", "move":
		kv = append(kv, "paths", listPaths(p), "dest", "/"+p.Dest)
	case "chmod":
		kv = append(kv, "path", "/"+p.Path, "mode", p.Mode)
	case "zip", "tar":
		kv = append(kv, "archive", "/"+joinRel(p.Dest, p.Name), "paths", listPaths(p))
	case "extract":
		kv = append(kv, "path", "/"+p.Path, "subfolder", p.ToSubfolder)
	case "save":
		size := 0
		if p.Content != nil {
			size = len(*p.Content)
		}
		kv = append(kv, "path", "/"+p.Path, "size", size)
	default:
		kv = append(kv, "path", "/"+p.Path)
	}
	if err != nil {
		auditWarn(op+" failed", append(kv, "err", err.Error())...)
		return
	}
	auditInfo(op, kv...)
}

func joinRel(dir, name string) string {
	if dir == "" {
		return name
	}
	return strings.TrimSuffix(dir, "/") + "/" + name
}

func listPaths(p api.Payload) string {
	ps := p.Paths
	if len(ps) == 0 {
		ps = []string{p.Path}
	}
	out := make([]string, len(ps))
	for i, x := range ps {
		out[i] = "/" + x
	}
	return strings.Join(out, ", ")
}

// statusWriter captures the response status for the trace log.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// traceRequests logs every HTTP request at -vv.
func (s *Server) traceRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if log.Verbosity() < 2 {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"bytes", sw.bytes, "ms", time.Since(start).Milliseconds(), "ip", s.clientIP(r))
	})
}

func auditInfo(msg string, kv ...any) { log.AuditInfo(msg, kv...) }
func auditWarn(msg string, kv ...any) { log.AuditWarn(msg, kv...) }
