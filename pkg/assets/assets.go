// Package assets serves the embedded web front-end straight from memory.
//
// The front-end is embedded as an encrypted blob (see package bundle), decoded
// once at start-up into RAM and never written to disk.
package assets

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/helviojunior/fmweb/pkg/assets/bundle"
)

//go:embed bundle.bin
var blob []byte

type file struct {
	data  []byte
	gz    []byte
	ctype string
	etag  string
}

var (
	once    sync.Once
	files   map[string]*file
	loadErr error
	modTime = time.Now()
)

// Load decodes the embedded bundle (idempotent).
func Load() error {
	once.Do(func() {
		raw, err := bundle.Open(blob)
		if err != nil {
			loadErr = err
			return
		}
		files = make(map[string]*file, len(raw))
		for name, data := range raw {
			sum := sha256.Sum256(data)
			f := &file{
				data:  data,
				ctype: contentType(name),
				etag:  `"` + hex.EncodeToString(sum[:8]) + `"`,
			}
			if compressible(f.ctype) && len(data) > 512 {
				var b bytes.Buffer
				zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
				zw.Write(data)
				zw.Close()
				if b.Len() < len(data) {
					f.gz = b.Bytes()
				}
			}
			files[name] = f
		}
		if _, ok := files["index.html"]; !ok {
			loadErr = errNoIndex
		}
	})
	return loadErr
}

type constErr string

func (e constErr) Error() string { return string(e) }

const errNoIndex = constErr("asset bundle has no index.html")

// Count returns the number of embedded files.
func Count() int { return len(files) }

func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".json", ".map":
		return "application/json; charset=utf-8"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	}
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

func compressible(ct string) bool {
	return strings.HasPrefix(ct, "text/") || strings.Contains(ct, "javascript") ||
		strings.Contains(ct, "json") || strings.Contains(ct, "svg")
}

// Handler serves the front-end with SPA fallback to index.html.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		f, ok := files[name]
		if !ok {
			// SPA fallback: every client-side route (/files/…, /edit/a.txt,
			// /view/b.pdf) gets the app shell; only missing build assets 404.
			if !isAppRoute(name) {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
			f = files[name]
		}
		h := w.Header()
		h.Set("Content-Type", f.ctype)
		h.Set("ETag", f.etag)
		h.Set("Vary", "Accept-Encoding")
		if strings.HasPrefix(name, "assets/") {
			// Vite emits content-hashed file names under assets/.
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		body := f.data
		if f.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			h.Set("Content-Encoding", "gzip")
			body = f.gz
		}
		http.ServeContent(w, r, "", modTime, bytes.NewReader(body))
	})
}

// IsPageRequest reports whether a GET for urlPath is a page load (served by
// the app shell) rather than a static build asset.
func IsPageRequest(urlPath string) bool {
	name := strings.TrimPrefix(path.Clean("/"+urlPath), "/")
	if strings.HasPrefix(name, "assets/") {
		return false
	}
	if _, isFile := files[name]; isFile {
		return name == "index.html"
	}
	return isAppRoute(name)
}

// isAppRoute: client-side routes of the SPA. File-like names outside them
// (favicon.ico, robots.txt, …) are a plain 404, not the app shell.
func isAppRoute(name string) bool {
	if strings.HasPrefix(name, "assets/") {
		return false
	}
	for _, p := range []string{"files/", "edit/", "view/"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return path.Ext(name) == ""
}
