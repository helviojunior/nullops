package fileops

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Document is a text file opened in the editor. Content always uses "\n"; the
// original encoding (UTF-8, UTF-8 with BOM, UTF-16 LE/BE with BOM) and line
// ending (LF / CRLF) are reported so that saving writes them back unchanged.
type Document struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"` // utf-8 | utf-8-bom | utf-16le | utf-16be
	EOL      string `json:"eol"`      // lf | crlf | mixed
	Hash     string `json:"hash"`     // sha256 of the bytes on disk (conflict detection)
}

// ConflictError is returned by WriteDocument when the file changed on disk
// since it was opened.
type ConflictError struct{}

func (ConflictError) Error() string { return "The file was changed on disk since it was opened" }

var (
	bomUTF8    = []byte{0xef, 0xbb, 0xbf}
	bomUTF16LE = []byte{0xff, 0xfe}
	bomUTF16BE = []byte{0xfe, 0xff}
)

func hashBytes(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// decodeText converts raw file bytes to a Go string and names the encoding.
func decodeText(raw []byte) (string, string, bool) {
	switch {
	case bytes.HasPrefix(raw, bomUTF8):
		b := raw[3:]
		return string(b), "utf-8-bom", utf8.Valid(b) && !bytes.Contains(b, []byte{0})
	case bytes.HasPrefix(raw, bomUTF16LE), bytes.HasPrefix(raw, bomUTF16BE):
		le := bytes.HasPrefix(raw, bomUTF16LE)
		b := raw[2:]
		if len(b)%2 != 0 {
			return "", "", false
		}
		u := make([]uint16, len(b)/2)
		for i := range u {
			if le {
				u[i] = binary.LittleEndian.Uint16(b[2*i:])
			} else {
				u[i] = binary.BigEndian.Uint16(b[2*i:])
			}
		}
		s := string(utf16.Decode(u))
		enc := "utf-16be"
		if le {
			enc = "utf-16le"
		}
		return s, enc, !strings.ContainsRune(s, 0)
	}
	return string(raw), "utf-8", utf8.Valid(raw) && !bytes.Contains(raw, []byte{0})
}

func encodeText(s, enc string) ([]byte, error) {
	switch enc {
	case "", "utf-8":
		return []byte(s), nil
	case "utf-8-bom":
		return append(append([]byte{}, bomUTF8...), s...), nil
	case "utf-16le", "utf-16be":
		u := utf16.Encode([]rune(s))
		out := make([]byte, 2+2*len(u))
		if enc == "utf-16le" {
			copy(out, bomUTF16LE)
			for i, c := range u {
				binary.LittleEndian.PutUint16(out[2+2*i:], c)
			}
		} else {
			copy(out, bomUTF16BE)
			for i, c := range u {
				binary.BigEndian.PutUint16(out[2+2*i:], c)
			}
		}
		return out, nil
	}
	return nil, errf("Unsupported encoding")
}

func detectEOL(s string) string {
	crlf := strings.Count(s, "\r\n")
	lf := strings.Count(s, "\n") - crlf
	switch {
	case crlf > 0 && lf == 0:
		return "crlf"
	case crlf > 0 && lf > 0:
		return "mixed"
	}
	return "lf"
}

// ReadDocument opens a text file for the editor.
func (f *FS) ReadDocument(rel string) (*Document, error) {
	fh, st, err := f.OpenFile(rel)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	if st.Size() > MaxEditSize {
		return nil, errf("File too large to edit")
	}
	raw, err := io.ReadAll(fh)
	if err != nil {
		return nil, errf("Cannot read file")
	}
	text, enc, ok := decodeText(raw)
	if !ok {
		return nil, errf("File is not valid UTF-8 text")
	}
	eol := detectEOL(text)
	if eol == "crlf" {
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	return &Document{Path: rel, Content: text, Encoding: enc, EOL: eol, Hash: hashBytes(raw)}, nil
}

// WriteDocument saves editor content with the given encoding/EOL. When
// expectHash is set and the file on disk no longer matches it, a
// ConflictError is returned unless force is true.
func (f *FS) WriteDocument(rel, content, enc, eol, expectHash string, force bool) (*Entry, string, error) {
	if len(content) > MaxEditSize*2 {
		return nil, "", errf("Content too large")
	}
	ap, err := f.rpath(rel)
	if err != nil {
		return nil, "", err
	}
	if ap == f.root {
		return nil, "", errf("Invalid path")
	}
	if st, err := os.Stat(filepath.Dir(ap)); err != nil || !st.IsDir() {
		return nil, "", errf("Parent directory does not exist")
	}
	if st, err := os.Stat(ap); err == nil && st.IsDir() {
		return nil, "", errf("Is a directory")
	}
	if expectHash != "" && !force {
		if cur, err := os.ReadFile(ap); err == nil && hashBytes(cur) != expectHash {
			return nil, "", ConflictError{}
		}
	}
	if eol == "crlf" {
		content = strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\n", "\r\n")
	}
	raw, err := encodeText(content, enc)
	if err != nil {
		return nil, "", err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(ap); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(ap, raw, mode); err != nil {
		return nil, "", errf("Write failed")
	}
	return f.entry(ap), hashBytes(raw), nil
}
