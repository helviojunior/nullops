// Package fileops implements every filesystem operation, confined to a root
// directory. All public methods take client-relative, slash-separated paths
// and refuse anything that escapes the root (path traversal, symlinks).
package fileops

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Error is a user-facing filesystem error.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func errf(msg string) error { return &Error{Msg: msg} }

// ErrInvalidPath is returned for any path escaping the root.
var ErrInvalidPath = errf("Invalid path")

// cleanRel normalizes a client path into a clean relative slash path ("" = root).
func cleanRel(rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if strings.ContainsRune(rel, 0) {
		return "\x00"
	}
	c := path.Clean("/" + rel)
	return strings.TrimPrefix(c, "/")
}

// within reports whether p is root or below it.
func within(root, p string) bool {
	if p == root {
		return true
	}
	r := root
	if !strings.HasSuffix(r, string(os.PathSeparator)) {
		r += string(os.PathSeparator)
	}
	return strings.HasPrefix(p, r)
}

// realpath resolves symlinks of the deepest existing ancestor of p and joins
// the non-existing remainder (like Python's os.path.realpath).
func realpath(p string) (string, error) {
	p = filepath.Clean(p)
	var tail []string
	cur := p
	for {
		r, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				r = filepath.Join(r, tail[i])
			}
			return r, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
	}
}

// rpath resolves rel fully (following symlinks) and checks containment.
// Used whenever the *contents* are read, written or listed.
func (f *FS) rpath(rel string) (string, error) {
	c := cleanRel(rel)
	if c == "\x00" {
		return "", ErrInvalidPath
	}
	if c == "" {
		return f.root, nil
	}
	r, err := realpath(filepath.Join(f.root, filepath.FromSlash(c)))
	if err != nil || !within(f.root, r) {
		return "", ErrInvalidPath
	}
	return r, nil
}

// lpath resolves the parent of rel but not its last component, so that
// operations on the entry itself (delete, rename, move) act on a symlink and
// not on its target.
func (f *FS) lpath(rel string) (string, error) {
	c := cleanRel(rel)
	if c == "\x00" {
		return "", ErrInvalidPath
	}
	if c == "" {
		return f.root, nil
	}
	dir, base := path.Split(c)
	parent, err := f.rpath(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, base), nil
}

// relOf converts an absolute path under root back to a client path.
func (f *FS) relOf(abs string) string {
	r, err := filepath.Rel(f.root, abs)
	if err != nil || r == "." {
		return ""
	}
	return filepath.ToSlash(r)
}

// ValidateName rejects names that are empty, dot entries, contain separators
// or invisible characters (control, bidi, zero-width), are Windows device
// names, or are too long.
func ValidateName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, "/\\\x00") || len(name) > 255 ||
		unsafeNameChars(name) || reservedBase(name) {
		return errf("Invalid name")
	}
	return nil
}
