package fileops

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Uploads are written to a hidden part file named after a client-chosen
// upload id (derived from name, size and modification time), so an upload
// interrupted by a dropped connection can be resumed from where it stopped:
//
//	.nullops-upload-<id>.part  (never listed, renamed to the final name at the end)
const partPrefix, partSuffix = ".nullops-upload-", ".part"

var uploadIDRe = regexp.MustCompile(`^[0-9a-f]{16,64}$`)

// IsUploadPart reports whether name is an in-progress upload.
func IsUploadPart(name string) bool {
	return strings.HasPrefix(name, partPrefix) && strings.HasSuffix(name, partSuffix)
}

// UploadRequest is one chunk of an upload.
type UploadRequest struct {
	Dir      string
	Name     string
	ID       string // upload id; empty = derived from the name (no resume across files)
	Offset   int64
	Total    int64 // full file size (-1 = unknown)
	Data     []byte
	Last     bool
	MaxSize  int64 // 0 = unlimited
	AllowExt []string
}

func (f *FS) uploadTarget(dirRel, name, id string, allowExt []string) (dir, final, part string, err error) {
	if err := ValidateName(name); err != nil {
		return "", "", "", err
	}
	if IsUploadPart(name) {
		return "", "", "", errf("Invalid name")
	}
	if len(allowExt) > 0 {
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		ok := false
		for _, a := range allowExt {
			if strings.TrimPrefix(strings.ToLower(a), ".") == ext {
				ok = true
				break
			}
		}
		if !ok {
			return "", "", "", errf("File extension not allowed")
		}
	}
	if id == "" {
		sum := sha256.Sum256([]byte(name))
		id = hex.EncodeToString(sum[:16])
	}
	if !uploadIDRe.MatchString(id) {
		return "", "", "", errf("Invalid upload id")
	}
	dir, err = f.rpath(dirRel)
	if err != nil {
		return "", "", "", err
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", "", "", errf("Destination is not a directory")
	}
	final = filepath.Join(dir, name)
	part = filepath.Join(dir, partPrefix+id+partSuffix)
	return dir, final, part, nil
}

// UploadStatus returns how many bytes of an interrupted upload are already
// on the server (0 when there is nothing to resume).
func (f *FS) UploadStatus(dirRel, name, id string) (int64, error) {
	_, _, part, err := f.uploadTarget(dirRel, name, id, nil)
	if err != nil {
		return 0, err
	}
	st, err := os.Lstat(part)
	if err != nil || !st.Mode().IsRegular() {
		return 0, nil
	}
	return st.Size(), nil
}

// UploadChunk writes one chunk at its offset and, on the last chunk, renames
// the part file to the final name.
func (f *FS) UploadChunk(u UploadRequest) (*Entry, int64, error) {
	if u.Offset < 0 {
		return nil, 0, errf("Invalid offset")
	}
	end := u.Offset + int64(len(u.Data))
	if u.MaxSize > 0 && (u.Total > u.MaxSize || end > u.MaxSize) {
		return nil, 0, errf("File too large")
	}
	if u.Total >= 0 && end > u.Total {
		return nil, 0, errf("Invalid offset")
	}
	_, final, part, err := f.uploadTarget(u.Dir, u.Name, u.ID, u.AllowExt)
	if err != nil {
		return nil, 0, err
	}
	if st, err := os.Lstat(final); err == nil && (st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
		return nil, 0, errf("Target already exists")
	}
	if st, err := os.Lstat(part); err == nil && !st.Mode().IsRegular() {
		return nil, 0, errf("Invalid upload state")
	}
	flags := os.O_WRONLY | os.O_CREATE
	if u.Offset == 0 {
		flags |= os.O_TRUNC
	}
	fh, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return nil, 0, errf("Cannot write file")
	}
	if u.Offset > 0 {
		st, err := fh.Stat()
		if err != nil || st.Size() < u.Offset {
			fh.Close()
			return nil, 0, errf("Upload out of order")
		}
	}
	if _, err := fh.WriteAt(u.Data, u.Offset); err != nil {
		fh.Close()
		return nil, 0, errf("Cannot write file")
	}
	if err := fh.Close(); err != nil {
		return nil, 0, errf("Cannot write file")
	}
	if !u.Last {
		return nil, end, nil
	}
	if err := os.Truncate(part, end); err != nil {
		return nil, 0, errf("Cannot write file")
	}
	_ = os.Chmod(part, 0o644)
	os.Remove(final)
	if err := os.Rename(part, final); err != nil {
		return nil, 0, errf("Cannot finalize upload")
	}
	return f.entry(final), end, nil
}
