package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP (RFC 6238): HMAC-SHA1, 30-second steps, 6 digits — the defaults every
// authenticator app (Google Authenticator, Authy, 1Password, …) understands.
const (
	totpPeriod = 30
	totpDigits = 6
	totpSkew   = 1 // accept one step before/after (clock drift)
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a random 160-bit secret, base32 encoded.
func NewTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return b32.EncodeToString(b), nil
}

func totpAt(key []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%1000000)
}

// TOTPCode returns the code for t (used by tests).
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	return totpAt(key, t.Unix()/totpPeriod), nil
}

// VerifyTOTP checks code at time t. It returns the matched time step, which
// the caller must remember to refuse replays of the same code.
func VerifyTOTP(secret, code string, t time.Time) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0, false
	}
	key, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(key) == 0 {
		return 0, false
	}
	now := t.Unix() / totpPeriod
	var matched int64
	ok := 0
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		if subtle.ConstantTimeCompare([]byte(totpAt(key, now+d)), []byte(code)) == 1 {
			matched = now + d
			ok = 1
		}
	}
	return matched, ok == 1
}

// TOTPURI is the otpauth:// URI encoded in the enrollment QR code.
func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprint(totpDigits))
	v.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + v.Encode()
}
