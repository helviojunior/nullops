package fileops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
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
	// Unique: never replace an existing file; the upload is stored as
	// name-EPOCH.ext (public upload links).
	Unique bool
}

func (f *FS) uploadTarget(dirRel, name, id string, allowExt []string) (dir, final, part string, err error) {
	// names of uploaded files come from the client: make them safe instead of
	// failing (status, chunks and finalize all see the same name)
	name = SanitizeName(name)
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
	u.Name = SanitizeName(u.Name)
	_, final, part, err := f.uploadTarget(u.Dir, u.Name, u.ID, u.AllowExt)
	if err != nil {
		return nil, 0, err
	}
	if st, err := os.Lstat(final); !u.Unique && err == nil && (st.IsDir() || st.Mode()&os.ModeSymlink != 0) {
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
	if u.Unique {
		dest, err := placeUnique(part, filepath.Dir(final), u.Name, time.Now().Unix())
		if err != nil {
			return nil, 0, errf("Cannot finalize upload")
		}
		return f.entry(dest), end, nil
	}
	os.Remove(final)
	if err := os.Rename(part, final); err != nil {
		return nil, 0, errf("Cannot finalize upload")
	}
	return f.entry(final), end, nil
}

// doubleExt keeps compound extensions together: a.tar.gz -> a-EPOCH.tar.gz.
var doubleExt = []string{".tar.gz", ".tar.bz2", ".tar.xz", ".tar.zst"}

// UniqueName returns "stem-EPOCH.ext" (with "-n" before the extension when n > 0).
func UniqueName(name string, epoch int64, n int) string {
	stem, ext := name, filepath.Ext(name)
	low := strings.ToLower(name)
	for _, d := range doubleExt {
		if strings.HasSuffix(low, d) && len(name) > len(d) {
			ext = name[len(name)-len(d):]
			break
		}
	}
	stem = name[:len(name)-len(ext)]
	if stem == "" { // ".bashrc"
		stem, ext = name, ""
	}
	suffix := fmt.Sprintf("-%d", epoch)
	if n > 0 {
		suffix += fmt.Sprintf("-%d", n)
	}
	return stem + suffix + ext
}

// placeUnique moves src to a name that does not exist yet in dir. A hard
// link fails atomically when the target exists, so nothing is ever replaced,
// even with concurrent uploads; on filesystems without hard links it falls
// back to check-then-rename.
func placeUnique(src, dir, name string, epoch int64) (string, error) {
	for n := 0; n < 1000; n++ {
		dest := filepath.Join(dir, UniqueName(name, epoch, n))
		err := os.Link(src, dest)
		if err == nil {
			os.Remove(src)
			return dest, nil
		}
		if os.IsExist(err) {
			continue
		}
		if _, statErr := os.Lstat(dest); statErr == nil {
			continue
		}
		if err := os.Rename(src, dest); err != nil {
			return "", err
		}
		return dest, nil
	}
	return "", errf("Cannot finalize upload")
}
