package fileops

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Options configures an FS.
type Options struct {
	ShowHidden bool
	Exclude    []string // glob patterns hidden from listings
}

// FS is a root-confined filesystem.
type FS struct {
	root string
	opt  Options
}

// Entry describes one file or directory.
type Entry struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	IsDir  bool   `json:"is_dir"`
	IsLink bool   `json:"is_link"`
	Size   int64  `json:"size"`
	MTime  int64  `json:"mtime"`
	MTimeS string `json:"mtime_str"`
	Mode   uint32 `json:"mode"`
	Perms  string `json:"perms"`
}

// Listing is the result of List.
type Listing struct {
	Path      string   `json:"path"`
	Entries   []*Entry `json:"entries"`
	DirCount  int      `json:"dir_count"`
	FileCount int      `json:"file_count"`
	TotalSize int64    `json:"total_size"`
}

// New creates an FS rooted at root (resolved to its real path).
func New(root string, opt Options) (*FS, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	return &FS{root: real, opt: opt}, nil
}

// Root returns the absolute root directory.
func (f *FS) Root() string { return f.root }

func (f *FS) hidden(name, rel string) bool {
	if IsUploadPart(name) {
		return true
	}
	if !f.opt.ShowHidden && strings.HasPrefix(name, ".") {
		return true
	}
	for _, p := range f.opt.Exclude {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
		if ok, _ := path.Match(p, rel); ok {
			return true
		}
	}
	return false
}

func (f *FS) entry(abs string) *Entry {
	lst, err := os.Lstat(abs)
	if err != nil {
		return nil
	}
	isLink := lst.Mode()&os.ModeSymlink != 0
	st := lst
	if isLink {
		if t, err := os.Stat(abs); err == nil {
			st = t
		}
	}
	e := &Entry{
		Name:   filepath.Base(abs),
		Path:   f.relOf(abs),
		IsDir:  st.IsDir(),
		IsLink: isLink,
		MTime:  st.ModTime().Unix(),
		MTimeS: st.ModTime().Local().Format("2006-01-02 15:04"),
		Mode:   uint32(st.Mode().Perm()),
		Perms:  permString(st.Mode()),
	}
	if !e.IsDir {
		e.Size = st.Size()
	}
	if abs == f.root {
		e.Name = "/"
	}
	return e
}

func permString(m fs.FileMode) string {
	s := m.String()
	// Go renders e.g. "-rwxr-xr-x", "drwxr-xr-x", "Lrwxrwxrwx"; keep the last 10.
	if len(s) > 10 {
		s = s[len(s)-10:]
	}
	return s
}

// List lists a directory.
func (f *FS) List(rel string) (*Listing, error) {
	base, err := f.rpath(rel)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(base)
	if err != nil || !st.IsDir() {
		return nil, errf("Not a directory")
	}
	des, err := os.ReadDir(base)
	if err != nil {
		return nil, errf("Cannot read directory")
	}
	var dirs, files []*Entry
	var total int64
	for _, de := range des {
		ap := filepath.Join(base, de.Name())
		if f.hidden(de.Name(), f.relOf(ap)) {
			continue
		}
		e := f.entry(ap)
		if e == nil {
			continue
		}
		if e.IsDir {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
			total += e.Size
		}
	}
	byName := func(s []*Entry) {
		sort.Slice(s, func(i, j int) bool {
			return strings.ToLower(s[i].Name) < strings.ToLower(s[j].Name)
		})
	}
	byName(dirs)
	byName(files)
	return &Listing{
		Path:      f.relOf(base),
		Entries:   append(append([]*Entry{}, dirs...), files...),
		DirCount:  len(dirs),
		FileCount: len(files),
		TotalSize: total,
	}, nil
}

// Stat returns one entry.
func (f *FS) Stat(rel string) (*Entry, error) {
	ap, err := f.lpath(rel)
	if err != nil {
		return nil, err
	}
	e := f.entry(ap)
	if e == nil {
		return nil, errf("Not found")
	}
	return e, nil
}

// OpenFile opens a regular file for reading (download).
func (f *FS) OpenFile(rel string) (*os.File, os.FileInfo, error) {
	ap, err := f.rpath(rel)
	if err != nil {
		return nil, nil, err
	}
	st, err := os.Stat(ap)
	if err != nil || !st.Mode().IsRegular() {
		return nil, nil, errf("Not a file")
	}
	fh, err := os.Open(ap)
	if err != nil {
		return nil, nil, errf("Cannot open file")
	}
	return fh, st, nil
}

// MaxEditSize caps the editor payload.
const MaxEditSize = 5 * 1024 * 1024

// ReadText reads a UTF-8 text file for the editor.
func (f *FS) ReadText(rel string) (string, error) {
	fh, st, err := f.OpenFile(rel)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	if st.Size() > MaxEditSize {
		return "", errf("File too large to edit")
	}
	b, err := io.ReadAll(fh)
	if err != nil {
		return "", errf("Cannot read file")
	}
	if !isUTF8(b) {
		return "", errf("File is not valid UTF-8 text")
	}
	return string(b), nil
}

// WriteText saves editor content.
func (f *FS) WriteText(rel, content string) (*Entry, error) {
	if len(content) > MaxEditSize*2 {
		return nil, errf("Content too large")
	}
	ap, err := f.rpath(rel)
	if err != nil {
		return nil, err
	}
	if ap == f.root {
		return nil, errf("Invalid path")
	}
	if st, err := os.Stat(filepath.Dir(ap)); err != nil || !st.IsDir() {
		return nil, errf("Parent directory does not exist")
	}
	if st, err := os.Stat(ap); err == nil && st.IsDir() {
		return nil, errf("Is a directory")
	}
	if err := os.WriteFile(ap, []byte(content), 0o644); err != nil {
		return nil, errf("Write failed")
	}
	return f.entry(ap), nil
}

func (f *FS) newChild(dirRel, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	dir, err := f.rpath(dirRel)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", errf("Destination is not a directory")
	}
	ap := filepath.Join(dir, name)
	if _, err := os.Lstat(ap); err == nil {
		return "", errf("Already exists")
	}
	return ap, nil
}

// Mkdir creates a folder.
func (f *FS) Mkdir(dirRel, name string) (*Entry, error) {
	ap, err := f.newChild(dirRel, name)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(ap, 0o755); err != nil {
		return nil, errf("Cannot create folder")
	}
	return f.entry(ap), nil
}

// Touch creates an empty file.
func (f *FS) Touch(dirRel, name string) (*Entry, error) {
	ap, err := f.newChild(dirRel, name)
	if err != nil {
		return nil, err
	}
	fh, err := os.OpenFile(ap, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, errf("Cannot create file")
	}
	fh.Close()
	return f.entry(ap), nil
}

// Rename renames an entry inside its folder.
func (f *FS) Rename(rel, newName string) (*Entry, error) {
	if err := ValidateName(newName); err != nil {
		return nil, err
	}
	ap, err := f.lpath(rel)
	if err != nil {
		return nil, err
	}
	if ap == f.root {
		return nil, errf("Cannot rename the root")
	}
	if _, err := os.Lstat(ap); err != nil {
		return nil, errf("Not found")
	}
	target := filepath.Join(filepath.Dir(ap), newName)
	if _, err := os.Lstat(target); err == nil {
		return nil, errf("Target already exists")
	}
	if err := os.Rename(ap, target); err != nil {
		return nil, errf("Rename failed")
	}
	return f.entry(target), nil
}

// Delete removes a file, symlink or folder (recursively).
func (f *FS) Delete(rel string) error {
	ap, err := f.lpath(rel)
	if err != nil {
		return err
	}
	if ap == f.root {
		return errf("Refusing to delete the root")
	}
	if _, err := os.Lstat(ap); err != nil {
		return errf("Not found")
	}
	if err := os.RemoveAll(ap); err != nil {
		return errf("Delete failed")
	}
	return nil
}

func (f *FS) destFor(src, destRel string) (string, error) {
	dir, err := f.rpath(destRel)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", errf("Destination is not a directory")
	}
	dest := filepath.Join(dir, filepath.Base(src))
	if _, err := os.Lstat(dest); err == nil {
		return "", errf("Target already exists")
	}
	if within(src, dest) {
		return "", errf("Cannot copy or move a folder into itself")
	}
	return dest, nil
}

// Copy copies an entry into destRel.
func (f *FS) Copy(rel, destRel string) (*Entry, error) {
	src, err := f.lpath(rel)
	if err != nil {
		return nil, err
	}
	if src == f.root {
		return nil, errf("Cannot copy the root")
	}
	if _, err := os.Lstat(src); err != nil {
		return nil, errf("Source not found")
	}
	dest, err := f.destFor(src, destRel)
	if err != nil {
		return nil, err
	}
	if err := copyTree(src, dest); err != nil {
		return nil, errf("Copy failed")
	}
	return f.entry(dest), nil
}

// Move moves an entry into destRel.
func (f *FS) Move(rel, destRel string) (*Entry, error) {
	src, err := f.lpath(rel)
	if err != nil {
		return nil, err
	}
	if src == f.root {
		return nil, errf("Cannot move the root")
	}
	if _, err := os.Lstat(src); err != nil {
		return nil, errf("Source not found")
	}
	dest, err := f.destFor(src, destRel)
	if err != nil {
		return nil, err
	}
	if err := os.Rename(src, dest); err != nil {
		// cross-device: copy then remove
		if err := copyTree(src, dest); err != nil {
			return nil, errf("Move failed")
		}
		if err := os.RemoveAll(src); err != nil {
			return nil, errf("Move failed")
		}
	}
	return f.entry(dest), nil
}

// Chmod changes permission bits.
func (f *FS) Chmod(rel string, mode uint32) (*Entry, error) {
	ap, err := f.rpath(rel)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(ap); err != nil {
		return nil, errf("Not found")
	}
	if err := os.Chmod(ap, fs.FileMode(mode&0o777)); err != nil {
		return nil, errf("chmod failed")
	}
	return f.entry(ap), nil
}

// SearchLimit caps search results.
const SearchLimit = 500

// Search finds entries whose name contains query (case-insensitive).
func (f *FS) Search(rel, query string) ([]*Entry, error) {
	base, err := f.rpath(rel)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := []*Entry{}
	if q == "" {
		return out, nil
	}
	stop := errors.New("stop")
	err = filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == base {
			return nil
		}
		if f.hidden(d.Name(), f.relOf(p)) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.Contains(strings.ToLower(d.Name()), q) {
			if e := f.entry(p); e != nil {
				out = append(out, e)
				if len(out) >= SearchLimit {
					return stop
				}
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, stop) {
		return nil, errf("Search failed")
	}
	return out, nil
}

// copyTree copies files and folders; symlinks are skipped (never followed).
func copyTree(src, dest string) error {
	st, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case st.Mode()&os.ModeSymlink != 0:
		return nil
	case st.IsDir():
		if err := os.Mkdir(dest, st.Mode().Perm()|0o700); err != nil {
			return err
		}
		des, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, de := range des {
			if err := copyTree(filepath.Join(src, de.Name()), filepath.Join(dest, de.Name())); err != nil {
				return err
			}
		}
		return nil
	case st.Mode().IsRegular():
		return copyFile(src, dest, st)
	}
	return nil
}

func copyFile(src, dest string, st os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dest, time.Now(), st.ModTime())
}

func isUTF8(b []byte) bool {
	return utf8.Valid(b) && !bytes.Contains(b, []byte{0})
}
