package fileops

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// ProbeSize is how many leading bytes are inspected to detect the MIME type.
const ProbeSize = 4096

// Probe is the result of a content sniff.
type Probe struct {
	Path   string `json:"path"`
	Mime   string `json:"mime"`
	IsText bool   `json:"is_text"` // text/* and editable (UTF-8, no binary bytes)
	Size   int64  `json:"size"`
}

// Probe reads the first ProbeSize bytes of a file and detects its MIME type.
// The UI opens text/* files in the viewer/editor and downloads the rest.
func (f *FS) Probe(rel string) (*Probe, error) {
	fh, st, err := f.OpenFile(rel)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	buf := make([]byte, ProbeSize)
	n, err := io.ReadFull(fh, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, errf("Cannot read file")
	}
	mime, isText := DetectMime(buf[:n])
	return &Probe{Path: rel, Mime: mime, IsText: isText, Size: st.Size()}, nil
}

// DetectMime sniffs buf (the start of a file). It uses the WHATWG algorithm
// of net/http (which looks at the first 512 bytes) and then validates text
// over the whole buffer: a text/* result must be valid UTF-8 without binary
// control bytes, and unknown content that passes that check is text/plain.
func DetectMime(buf []byte) (mime string, isText bool) {
	mime = http.DetectContentType(buf)
	base := strings.TrimSpace(strings.SplitN(mime, ";", 2)[0])
	charset := ""
	if i := strings.Index(strings.ToLower(mime), "charset="); i >= 0 {
		charset = strings.ToLower(strings.TrimSpace(mime[i+len("charset="):]))
	}
	switch {
	case strings.HasPrefix(base, "text/"):
		if strings.HasPrefix(charset, "utf-16") {
			// UTF-16 with BOM (common in Windows scripts): editable, the
			// editor writes it back as UTF-16.
			_, _, ok := decodeText(evenLen(buf))
			return mime, ok
		}
		if looksText(buf) {
			if looksSVG(buf) {
				// net/http sniffs SVG as text/xml: it is an image (still editable)
				return "image/svg+xml", true
			}
			return mime, true
		}
		// binary bytes after the first 512: not really text
		return "application/octet-stream", false
	case base == "application/octet-stream" && looksText(buf):
		if looksSVG(buf) {
			return "image/svg+xml", true
		}
		return "text/plain; charset=utf-8", true
	}
	return mime, false
}

// looksText reports whether buf is UTF-8 text: no NUL, few control bytes. A
// multi-byte character cut at the end of the buffer is tolerated.
func looksText(buf []byte) bool {
	if bytes.IndexByte(buf, 0) >= 0 {
		return false
	}
	if !utf8.Valid(buf) {
		ok := false
		for k := 1; k <= 3 && k < len(buf); k++ {
			if utf8.Valid(buf[:len(buf)-k]) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	ctrl := 0
	for _, b := range buf {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' && b != '\f' && b != '\v' && b != 0x1b {
			ctrl++
		}
	}
	return ctrl*100 <= len(buf) // at most 1% control bytes
}

// evenLen trims a trailing odd byte (a UTF-16 unit cut by the probe size).
func evenLen(b []byte) []byte {
	if len(b)%2 == 1 {
		return b[:len(b)-1]
	}
	return b
}

// looksSVG reports whether a text buffer is an SVG document: it starts with
// an XML declaration, comment, doctype or the <svg> element itself, and has
// an <svg element in the inspected bytes.
func looksSVG(buf []byte) bool {
	b := bytes.TrimPrefix(buf, []byte{0xef, 0xbb, 0xbf})
	b = bytes.ToLower(bytes.TrimSpace(b))
	if !bytes.Contains(b, []byte("<svg")) {
		return false
	}
	for _, p := range []string{"<?xml", "<svg", "<!--", "<!doctype svg"} {
		if bytes.HasPrefix(b, []byte(p)) {
			return true
		}
	}
	return false
}
