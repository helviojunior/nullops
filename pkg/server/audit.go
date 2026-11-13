package server

import (
	"encoding/json"
	"net/http"
	"path"
	"strings"

	"github.com/helviojunior/fmweb/pkg/api"
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
func logAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && path.Ext(r.URL.Path) == "" {
			log.Info("access", "ip", clientIP(r), "path", r.URL.Path, "ua", userAgent(r))
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
func auditAction(r *http.Request, user, action string, plain []byte, err error) {
	op, ok := opNames[action]
	if !ok {
		return // list/stat/search/info are not logged
	}
	var p api.Payload
	_ = json.Unmarshal(plain, &p)
	kv := []any{"user", user, "ip", clientIP(r)}
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
		log.Warn(op+" failed", append(kv, "err", err.Error())...)
		return
	}
	log.Info(op, kv...)
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
