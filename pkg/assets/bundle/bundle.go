// Package bundle defines the obfuscated container used to embed the web
// front-end in the binary.
//
// Layout of the embedded blob:
//
//	salt (32 bytes) | nonce (12 bytes) | AES-256-GCM( zip archive of the files )
//
// The AES key is HKDF-SHA256(pepper, salt). The pepper is not stored as a
// contiguous constant; it is reassembled at runtime. This is obfuscation, not
// a security boundary: its purpose is that no HTML/JS/CSS appears in clear
// text inside the executable and nothing has to be extracted to disk.
package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	saltLen  = 32
	nonceLen = 12
	info     = "fmweb/assets/v1"
)

var (
	p1 = []byte{0x3a, 0x91, 0x5c, 0xe7, 0x02, 0x6d, 0xb4, 0x18, 0xcf, 0x77, 0x40, 0x9e, 0x25, 0xd3, 0x8a, 0x61}
	p2 = []byte{0x5f, 0xe2, 0x30, 0x84, 0x77, 0x0a, 0xd9, 0x6b, 0xa1, 0x14, 0x2f, 0xfd, 0x56, 0xb0, 0xe3, 0x0f}
	p3 = []byte{0x91, 0x0c, 0x6e, 0x23, 0xb8, 0x45, 0xfa, 0x37, 0x08, 0xd6, 0x7c, 0x51, 0xee, 0x29, 0x93, 0xc4}
)

// BuildPepper is a random per-build key (64 hex chars) injected by the
// Makefile with -ldflags "-X .../bundle.BuildPepper=..." and also given to
// tools/assetpack, so every build seals its front-end under a different key.
// Empty = built-in default.
var BuildPepper string

// SetPepper overrides the pepper (used by tools/assetpack).
func SetPepper(hexKey string) error {
	if hexKey != "" {
		if b, err := hex.DecodeString(hexKey); err != nil || len(b) != 32 {
			return errors.New("pepper must be 64 hex characters")
		}
	}
	BuildPepper = hexKey
	return nil
}

func pepper() []byte {
	out := make([]byte, 32)
	if b, err := hex.DecodeString(BuildPepper); err == nil && len(b) == 32 {
		// mixed with the built-in parts so the -X string alone is not the key
		for i := range out {
			out[i] = b[i] ^ p1[i%16] ^ p3[(i*5)%16]
		}
		return out
	}
	for i := 0; i < 16; i++ {
		out[i] = p1[i] ^ p3[15-i]
		out[16+i] = p2[i] ^ p1[(i*7)%16] ^ p3[i]
	}
	return out
}

func aead(salt []byte) (cipher.AEAD, error) {
	key, err := hkdf.Key(sha256.New, pepper(), salt, info, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// PackDir zips every regular file under dir and seals it.
func PackDir(dir string) ([]byte, int, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	if len(files) == 0 {
		return nil, 0, errors.New("no files found in " + dir)
	}
	sort.Strings(files)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range files {
		rel, _ := filepath.Rel(dir, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, 0, err
		}
		w, err := zw.CreateHeader(&zip.FileHeader{
			Name:     filepath.ToSlash(rel),
			Method:   zip.Deflate,
			Modified: time.Unix(0, 0).UTC(),
		})
		if err != nil {
			return nil, 0, err
		}
		if _, err := w.Write(data); err != nil {
			return nil, 0, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, 0, err
	}
	head := make([]byte, saltLen+nonceLen)
	if _, err := rand.Read(head); err != nil {
		return nil, 0, err
	}
	a, err := aead(head[:saltLen])
	if err != nil {
		return nil, 0, err
	}
	return a.Seal(head, head[saltLen:], buf.Bytes(), nil), len(files), nil
}

// Open decrypts a blob and returns the contained files, in memory only.
func Open(blob []byte) (map[string][]byte, error) {
	if len(blob) < saltLen+nonceLen+16 {
		return nil, errors.New("asset bundle is empty or truncated (run `make`)")
	}
	a, err := aead(blob[:saltLen])
	if err != nil {
		return nil, err
	}
	plain, err := a.Open(nil, blob[saltLen:saltLen+nonceLen], blob[saltLen+nonceLen:], nil)
	if err != nil {
		return nil, errors.New("asset bundle cannot be decrypted")
	}
	zr, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
	if err != nil {
		return nil, err
	}
	out := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		out[f.Name] = data
	}
	return out, nil
}
