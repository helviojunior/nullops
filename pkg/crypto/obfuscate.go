package crypto

// Obfuscated transport — the "obfuscated" channel mode.
//
// This is NOT encryption. It is a keystream XOR whose only job is to keep /api
// payloads out of cleartext on the wire when the channel runs without TLS and
// without the AES-GCM mode — e.g. a browser over plain http (where
// window.crypto.subtle is unavailable), or a legacy client without AES-GCM and
// HKDF (Windows PowerShell 5.1, a stdlib-only Python).
//
// Threat model, stated honestly:
//   - It defeats PASSIVE inspection that greps the traffic for strings, and
//     cleartext payloads in proxy/access logs.
//   - It does NOT resist an active MITM: without TLS the attacker can rewrite
//     the client code and the handshake anyway.
//   - Keyed on the session id (NewObfuscator), which itself travels in the
//     handshake response, it is COSMETIC: a deliberate observer who captured
//     the handshake can reproduce the keystream. Keyed on a pre-shared secret
//     the observer does not have (ObfSecret, from a high-entropy upload token),
//     a passive observer cannot recover the payload.
//
// Keystream (SHA-256 counter mode), per message/frame:
//
//	K_i       = SHA256("nullops/obf/v1" || secret || nonce || LE32(i)), i = 0,1,…
//	keystream = (K_0 || K_1 || …) truncated to len(plaintext)
//	wire      = nonce(12) || (plaintext XOR keystream)
//
// The envelope reuses {iv,data} and the binary frame reuses [4-byte BE len][12
// nonce][payload]: the wire shape is identical to the AES-GCM mode, and the
// codec is chosen by the session mode, not by the field names.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
)

// Codec is the payload transform of an /api session: AES-256-GCM (*Cipher),
// keystream XOR (*Obfuscator) or, when the session holds none, cleartext.
type Codec interface {
	Seal(plaintext []byte) Envelope
	SealJSON(v any) (Envelope, error)
	Open(env Envelope) ([]byte, error)
	Frame(plaintext []byte) []byte
	OpenFrame(iv, ct []byte) ([]byte, error)
	// FrameOverhead is the number of bytes Frame adds to each chunk (length
	// prefix + nonce + any auth tag), used to size Content-Length.
	FrameOverhead() int
}

var (
	_ Codec = (*Cipher)(nil)
	_ Codec = (*Obfuscator)(nil)
)

// obfInfo domain-separates the keystream from every other SHA-256 use.
var obfInfo = []byte("nullops/obf/v1")

var (
	obfTokenInfo = []byte("nullops/obf-token/v1")
	obfHintInfo  = []byte("nullops/obf-hint/v1")
)

// ObfSecret derives the keystream secret from a pre-shared upload token. Using
// it (instead of NewObfuscator on the session id) is what turns the obfuscated
// mode from cosmetic into genuine passive-capture resistance — but only when
// the token has enough entropy that a transmitted selector cannot be
// brute-forced offline.
func ObfSecret(token string) []byte {
	h := sha256.New()
	h.Write(obfTokenInfo)
	h.Write([]byte(token))
	return h.Sum(nil)
}

// ObfHint is a non-reversible selector a client may send in clear so the server
// can pick the matching token without the token itself appearing on the wire.
func ObfHint(token string) string {
	h := sha256.New()
	h.Write(obfHintInfo)
	h.Write([]byte(token))
	return B64.EncodeToString(h.Sum(nil)[:8])
}

// Obfuscator XORs payloads with a SHA-256 counter-mode keystream. See the file
// note: this is obfuscation, not encryption.
type Obfuscator struct {
	secret []byte
}

// NewObfuscator builds an obfuscator bound to secret (copied).
func NewObfuscator(secret []byte) *Obfuscator {
	return &Obfuscator{secret: append([]byte(nil), secret...)}
}

// Secret returns a copy of the raw secret.
func (o *Obfuscator) Secret() []byte { return append([]byte(nil), o.secret...) }

// xor applies the keystream to buf in place.
func (o *Obfuscator) xor(nonce, buf []byte) {
	var (
		block   [sha256.Size]byte
		ctrByte [4]byte
		ctr     uint32
	)
	off := sha256.Size // forces the first block to be generated
	for i := range buf {
		if off == sha256.Size {
			binary.LittleEndian.PutUint32(ctrByte[:], ctr)
			h := sha256.New()
			h.Write(obfInfo)
			h.Write(o.secret)
			h.Write(nonce)
			h.Write(ctrByte[:])
			h.Sum(block[:0])
			ctr++
			off = 0
		}
		buf[i] ^= block[off]
		off++
	}
}

// Seal obfuscates plaintext into an envelope (iv carries the nonce).
func (o *Obfuscator) Seal(plaintext []byte) Envelope {
	nonce := make([]byte, ivLen)
	_, _ = rand.Read(nonce)
	out := append([]byte(nil), plaintext...)
	o.xor(nonce, out)
	return Envelope{IV: B64.EncodeToString(nonce), Data: B64.EncodeToString(out)}
}

// SealJSON marshals v and obfuscates it.
func (o *Obfuscator) SealJSON(v any) (Envelope, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return Envelope{}, err
	}
	return o.Seal(b), nil
}

// Open de-obfuscates an envelope. There is no authentication tag: a tampered
// payload de-obfuscates to garbage rather than failing (obfuscation, not
// encryption).
func (o *Obfuscator) Open(env Envelope) ([]byte, error) {
	nonce, err := B64.DecodeString(env.IV)
	if err != nil || len(nonce) != ivLen {
		return nil, errors.New("bad nonce")
	}
	data, err := B64.DecodeString(env.Data)
	if err != nil {
		return nil, errors.New("bad data")
	}
	o.xor(nonce, data)
	return data, nil
}

// Frame obfuscates one chunk as a length-prefixed binary frame, matching the
// AES-GCM frame geometry: [4-byte BE len N][12 nonce][N bytes payload].
func (o *Obfuscator) Frame(plaintext []byte) []byte {
	out := make([]byte, 4+ivLen+len(plaintext))
	nonce := out[4 : 4+ivLen]
	_, _ = rand.Read(nonce)
	copy(out[4+ivLen:], plaintext)
	o.xor(nonce, out[4+ivLen:])
	binary.BigEndian.PutUint32(out[:4], uint32(len(plaintext)))
	return out
}

// OpenFrame de-obfuscates one binary frame — the inverse of Frame.
func (o *Obfuscator) OpenFrame(iv, ct []byte) ([]byte, error) {
	out := append([]byte(nil), ct...)
	o.xor(iv, out)
	return out, nil
}

// FrameOverhead is 4 (length prefix) + 12 (nonce); there is no auth tag.
func (o *Obfuscator) FrameOverhead() int { return 4 + ivLen }
