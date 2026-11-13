// Package crypto implements the encrypted transport of the /api channel.
//
// It mirrors the browser WebCrypto implementation (frontend/src/lib/crypto.js):
//
//	Handshake (per session, keys only in memory):
//	  1. Client sends its ephemeral ECDH P-256 public key (raw uncompressed
//	     point, base64url) to POST /api/handshake.
//	  2. Server generates its own ephemeral key pair, computes the ECDH shared
//	     secret and derives an AES-256-GCM key with HKDF-SHA256
//	     (salt = random session id bytes, info = "fmweb/aesgcm/v1").
//	  3. Server returns {session_id, server_pub}. The AES key never travels.
//
//	Envelope (both directions): {"iv": b64url(12 bytes), "data": b64url(ct||tag)}
//	Binary frames (downloads):  [4-byte BE len N][12-byte IV][N bytes ct||tag]
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
)

// HKDFInfo is the HKDF context string shared with the browser.
const HKDFInfo = "fmweb/aesgcm/v1"

const ivLen = 12

// B64 is unpadded base64url, as produced by the browser.
var B64 = base64.RawURLEncoding

// Envelope is the encrypted JSON body.
type Envelope struct {
	IV   string `json:"iv"`
	Data string `json:"data"`
}

// Cipher holds a derived session key.
type Cipher struct {
	aead cipher.AEAD
	key  []byte
}

// Key returns a copy of the raw AES key (used to persist the session wrapped).
func (c *Cipher) Key() []byte { return append([]byte(nil), c.key...) }

// Handshake runs the server side of the ECDH exchange. It returns the derived
// cipher and the server public key (raw uncompressed point).
func Handshake(clientPub, salt []byte) (*Cipher, []byte, error) {
	curve := ecdh.P256()
	peer, err := curve.NewPublicKey(clientPub)
	if err != nil {
		return nil, nil, err
	}
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	shared, err := priv.ECDH(peer)
	if err != nil {
		return nil, nil, err
	}
	key, err := hkdf.Key(sha256.New, shared, salt, HKDFInfo, 32)
	if err != nil {
		return nil, nil, err
	}
	c, err := NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	return c, priv.PublicKey().Bytes(), nil
}

// NewCipher builds an AES-256-GCM cipher from a raw key.
func NewCipher(key []byte) (*Cipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead, key: append([]byte(nil), key...)}, nil
}

// Seal encrypts plaintext into an envelope.
func (c *Cipher) Seal(plaintext []byte) Envelope {
	iv := make([]byte, ivLen)
	_, _ = rand.Read(iv)
	ct := c.aead.Seal(nil, iv, plaintext, nil)
	return Envelope{IV: B64.EncodeToString(iv), Data: B64.EncodeToString(ct)}
}

// SealJSON marshals v and encrypts it.
func (c *Cipher) SealJSON(v any) (Envelope, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return Envelope{}, err
	}
	return c.Seal(b), nil
}

// Open decrypts an envelope.
func (c *Cipher) Open(env Envelope) ([]byte, error) {
	iv, err := B64.DecodeString(env.IV)
	if err != nil || len(iv) != ivLen {
		return nil, errors.New("bad iv")
	}
	ct, err := B64.DecodeString(env.Data)
	if err != nil {
		return nil, errors.New("bad data")
	}
	return c.aead.Open(nil, iv, ct, nil)
}

// Frame encrypts one chunk as a length-prefixed binary frame.
func (c *Cipher) Frame(plaintext []byte) []byte {
	out := make([]byte, 4+ivLen, 4+ivLen+len(plaintext)+c.aead.Overhead())
	iv := out[4 : 4+ivLen]
	_, _ = rand.Read(iv)
	out = c.aead.Seal(out, iv, plaintext, nil)
	binary.BigEndian.PutUint32(out[:4], uint32(len(out)-4-ivLen))
	return out
}

// OpenFrame decrypts one binary frame (iv, ct||tag) — the inverse of Frame.
func (c *Cipher) OpenFrame(iv, ct []byte) ([]byte, error) {
	return c.aead.Open(nil, iv, ct, nil)
}
