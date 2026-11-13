// Package auth implements password hashing and the on-disk user store.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"golang.org/x/crypto/scrypt"
)

// scrypt parameters. The stored format is compatible with the Python version:
//
//	scrypt$<n>$<r>$<p>$<salt_hex>$<dk_hex>
const (
	scryptN   = 1 << 14
	scryptR   = 8
	scryptP   = 1
	scryptLen = 32
)

// HashPassword returns an scrypt hash string for password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk, err := scrypt.Key([]byte(password), salt, scryptN, scryptR, scryptP, scryptLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("scrypt$%d$%d$%d$%s$%s", scryptN, scryptR, scryptP,
		hex.EncodeToString(salt), hex.EncodeToString(dk)), nil
}

// VerifyPassword checks password against an scrypt hash in constant time.
func VerifyPassword(password, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 6 || parts[0] != "scrypt" {
		return false
	}
	n, err1 := strconv.Atoi(parts[1])
	r, err2 := strconv.Atoi(parts[2])
	p, err3 := strconv.Atoi(parts[3])
	salt, err4 := hex.DecodeString(parts[4])
	expected, err5 := hex.DecodeString(parts[5])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
		return false
	}
	// Refuse absurd parameters coming from a tampered file.
	if n <= 1 || n > 1<<20 || r <= 0 || r > 32 || p <= 0 || p > 16 || len(expected) == 0 {
		return false
	}
	dk, err := scrypt.Key([]byte(password), salt, n, r, p, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(dk, expected) == 1
}

// GeneratePassword returns a random alphanumeric password (CSPRNG).
func GeneratePassword(length int) (string, error) {
	const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, length)
	max := big.NewInt(int64(len(alphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}
