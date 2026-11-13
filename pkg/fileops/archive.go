package fileops

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MaxExtractSize caps the total uncompressed size of one extraction (zip bombs).
const MaxExtractSize int64 = 20 << 30 // 20 GiB

func (f *FS) archiveOut(destRel, name, defName string) (string, error) {
	if name == "" {
		name = defName
	}
	if err := ValidateName(name); err != nil {
		return "", err
	}
	dir, err := f.rpath(destRel)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", errf("Destination is not a directory")
	}
	out := filepath.Join(dir, name)
	if _, err := os.Lstat(out); err == nil {
		return "", errf("Archive already exists")
	}
	return out, nil
}

// walkSources calls fn for every regular file/dir under the given sources.
// Symlinks are skipped. arc is the slash path inside the archive.
func (f *FS) walkSources(rels []string, fn func(abs, arc string, d fs.DirEntry) error) error {
	if len(rels) == 0 {
		return errf("Nothing selected")
	}
	for _, rel := range rels {
		src, err := f.lpath(rel)
		if err != nil {
			return err
		}
		if src == f.root {
			return errf("Cannot archive the root")
		}
		parent := filepath.Dir(src)
		err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			r, _ := filepath.Rel(parent, p)
			return fn(p, filepath.ToSlash(r), d)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Zip creates a zip archive of rels inside destRel.
func (f *FS) Zip(rels []string, destRel, name string) (*Entry, error) {
	out, err := f.archiveOut(destRel, name, "archive_"+time.Now().Format("20060102_150405")+".zip")
	if err != nil {
		return nil, err
	}
	fh, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, errf("Cannot create archive")
	}
	zw := zip.NewWriter(fh)
	err = f.walkSources(rels, func(abs, arc string, d fs.DirEntry) error {
		if abs == out {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = arc
		if d.IsDir() {
			hdr.Name += "/"
			_, err = zw.CreateHeader(hdr)
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		return copyFrom(w, abs)
	})
	if cerr := zw.Close(); err == nil {
		err = cerr
	}
	if cerr := fh.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(out)
		return nil, asUserErr(err, "Zip failed")
	}
	return f.entry(out), nil
}

// Tar creates a .tar.gz archive of rels inside destRel.
func (f *FS) Tar(rels []string, destRel, name string) (*Entry, error) {
	out, err := f.archiveOut(destRel, name, "archive_"+time.Now().Format("20060102_150405")+".tar.gz")
	if err != nil {
		return nil, err
	}
	fh, err := os.OpenFile(out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, errf("Cannot create archive")
	}
	gz := gzip.NewWriter(fh)
	tw := tar.NewWriter(gz)
	err = f.walkSources(rels, func(abs, arc string, d fs.DirEntry) error {
		if abs == out {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !d.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = arc
		if d.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		return copyFrom(tw, abs)
	})
	for _, c := range []io.Closer{tw, gz, fh} {
		if cerr := c.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		os.Remove(out)
		return nil, asUserErr(err, "Tar failed")
	}
	return f.entry(out), nil
}

// Extract unpacks a zip / tar / tar.gz archive next to it (or into a
// subfolder named after it). Only regular files and folders are created;
// entries escaping the destination are rejected (zip-slip).
func (f *FS) Extract(rel string, toSubfolder bool) (*Entry, error) {
	src, err := f.rpath(rel)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(src); err != nil || !st.Mode().IsRegular() {
		return nil, errf("Archive not found")
	}
	dest := filepath.Dir(src)
	if toSubfolder {
		stem := filepath.Base(src)
		low := strings.ToLower(stem)
		for _, ext := range []string{".tar.gz", ".tgz", ".tar", ".zip"} {
			if strings.HasSuffix(low, ext) {
				stem = stem[:len(stem)-len(ext)]
				break
			}
		}
		dest = filepath.Join(dest, stem)
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return nil, errf("Cannot create folder")
		}
	}
	ex := &extractor{fs: f, dest: dest, budget: MaxExtractSize}
	if zr, err := zip.OpenReader(src); err == nil {
		defer zr.Close()
		err = ex.zip(&zr.Reader)
		if err != nil {
			return nil, asUserErr(err, "Extraction failed")
		}
		return f.entry(dest), nil
	}
	if err := ex.tarFile(src); err != nil {
		return nil, asUserErr(err, "Extraction failed")
	}
	return f.entry(dest), nil
}

type extractor struct {
	fs     *FS
	dest   string
	budget int64
}

func (e *extractor) target(name string) (string, error) {
	// Reject absolute names and any ".." component outright, then re-check
	// lexical containment after cleaning.
	n := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(n, "/") || strings.ContainsRune(n, 0) || filepath.VolumeName(n) != "" {
		return "", errf("Unsafe path in archive")
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return "", errf("Unsafe path in archive")
		}
	}
	c := cleanRel(n)
	t := filepath.Join(e.dest, filepath.FromSlash(c))
	if c == "" || !within(e.dest, t) {
		return "", errf("Unsafe path in archive")
	}
	// the parent must also resolve inside the root (no symlinked dirs)
	r, err := realpath(filepath.Dir(t))
	if err != nil || !within(e.fs.root, r) {
		return "", errf("Unsafe path in archive")
	}
	return t, nil
}

func (e *extractor) write(t string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(t), 0o755); err != nil {
		return err
	}
	if st, err := os.Lstat(t); err == nil && (st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
		return errf("Unsafe path in archive")
	}
	out, err := os.OpenFile(t, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm()|0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(r, e.budget+1))
	out.Close()
	if err != nil {
		return err
	}
	e.budget -= n
	if e.budget < 0 {
		return errf("Archive too large")
	}
	return nil
}

func (e *extractor) zip(zr *zip.Reader) error {
	// validate every name before writing anything
	for _, zf := range zr.File {
		if _, err := e.target(zf.Name); err != nil {
			return err
		}
	}
	for _, zf := range zr.File {
		t, err := e.target(zf.Name)
		if err != nil {
			return err
		}
		mode := zf.Mode()
		switch {
		case mode.IsDir() || strings.HasSuffix(zf.Name, "/"):
			if err := os.MkdirAll(t, 0o755); err != nil {
				return err
			}
		case mode.IsRegular():
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			err = e.write(t, rc, mode)
			rc.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *extractor) tarFile(src string) error {
	open := func() (*tar.Reader, func(), error) {
		fh, err := os.Open(src)
		if err != nil {
			return nil, nil, err
		}
		var r io.Reader = fh
		if gz, err := gzip.NewReader(fh); err == nil {
			r = gz
		} else {
			fh.Seek(0, io.SeekStart)
		}
		return tar.NewReader(r), func() { fh.Close() }, nil
	}
	// pass 1: validate names (and that this is a tar at all)
	tr, closeFn, err := open()
	if err != nil {
		return err
	}
	count := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			closeFn()
			if count == 0 {
				return errf("Unsupported archive format")
			}
			return err
		}
		count++
		if _, err := e.target(h.Name); err != nil {
			closeFn()
			return err
		}
	}
	closeFn()
	if count == 0 {
		return errf("Unsupported archive format")
	}
	// pass 2: extract
	tr, closeFn, err = open()
	if err != nil {
		return err
	}
	defer closeFn()
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		t, err := e.target(h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(t, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := e.write(t, tr, os.FileMode(h.Mode)); err != nil {
				return err
			}
		}
	}
}

func copyFrom(w io.Writer, abs string) error {
	fh, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = io.Copy(w, fh)
	return err
}

func asUserErr(err error, fallback string) error {
	if _, ok := err.(*Error); ok {
		return err
	}
	return errf(fallback)
}
