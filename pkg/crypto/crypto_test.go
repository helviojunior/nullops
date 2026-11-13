package crypto

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

func TestHandshakeAndEnvelope(t *testing.T) {
	client, _ := ecdh.P256().GenerateKey(rand.Reader)
	salt := make([]byte, 16)
	rand.Read(salt)
	srv, serverPub, err := Handshake(client.PublicKey().Bytes(), salt)
	if err != nil {
		t.Fatal(err)
	}
	peer, _ := ecdh.P256().NewPublicKey(serverPub)
	shared, _ := client.ECDH(peer)
	key, _ := hkdf.Key(sha256.New, shared, salt, HKDFInfo, 32)
	cli, _ := NewCipher(key)

	env, _ := srv.SealJSON(map[string]int{"a": 1})
	pt, err := cli.Open(env)
	if err != nil || string(pt) != `{"a":1}` {
		t.Fatal(err, string(pt))
	}
	frame := srv.Frame([]byte("chunk"))
	n := binary.BigEndian.Uint32(frame[:4])
	out, err := cli.aead.Open(nil, frame[4:16], frame[16:16+n], nil)
	if err != nil || !bytes.Equal(out, []byte("chunk")) {
		t.Fatal("frame mismatch")
	}
	if _, _, err := Handshake([]byte("bogus"), salt); err == nil {
		t.Fatal("bad public key accepted")
	}
}
