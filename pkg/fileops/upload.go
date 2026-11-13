package fileops

import (
	"os"
	"path/filepath"
	"strings"
)

// UploadChunk writes one chunk of an upload at offset into "<name>.part" and,
// on the last chunk, atomically renames it to the final name.
func (f *FS) UploadChunk(dirRel, name string, offset int64, data []byte, last bool, allowedExt []string) (*Entry, int64, error) {
	if err := ValidateName(name); err != nil {
		return nil, 0, err
	}
	if offset < 0 {
		return nil, 0, errf("Invalid offset")
	}
	if len(allowedExt) > 0 {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		ok := false
		for _, a := range allowedExt {
			if strings.TrimPrefix(strings.ToLower(a), ".") == ext {
				ok = true
				break
			}
		}
		if !ok {
			return nil, 0, errf("File extension not allowed")
		}
	}
	dir, err := f.rpath(dirRel)
	if err != nil {
		return nil, 0, err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, 0, errf("Destination is not a directory")
	}
	final := filepath.Join(dir, name)
	if st, err := os.Lstat(final); err == nil && (st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
		return nil, 0, errf("Target already exists")
	}
	part := final + ".part"
	if st, err := os.Lstat(part); err == nil && !st.Mode().IsRegular() {
		return nil, 0, errf("Invalid upload state")
	}
	flags := os.O_WRONLY | os.O_CREATE
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	fh, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return nil, 0, errf("Cannot write file")
	}
	if offset > 0 {
		st, err := fh.Stat()
		if err != nil || st.Size() < offset {
			fh.Close()
			return nil, 0, errf("Upload out of order")
		}
	}
	if _, err := fh.WriteAt(data, offset); err != nil {
		fh.Close()
		return nil, 0, errf("Cannot write file")
	}
	if err := fh.Close(); err != nil {
		return nil, 0, errf("Cannot write file")
	}
	received := offset + int64(len(data))
	if !last {
		return nil, received, nil
	}
	if err := os.Truncate(part, received); err != nil {
		return nil, 0, errf("Cannot write file")
	}
	os.Remove(final)
	if err := os.Rename(part, final); err != nil {
		return nil, 0, errf("Cannot finalize upload")
	}
	return f.entry(final), received, nil
}
