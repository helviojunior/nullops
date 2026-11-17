package crypto

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestObfuscateRoundTrip(t *testing.T) {
	o := NewObfuscator([]byte("session-id-bytes"))
	for _, n := range []int{0, 1, 31, 32, 33, 100, 4096} {
		pt := bytes.Repeat([]byte{0xA5}, n)
		env := o.Seal(pt)
		got, err := o.Open(env)
		if err != nil {
			t.Fatalf("Open(len=%d): %v", n, err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("round trip mismatch at len=%d", n)
		}
		if n > 0 && bytes.Equal([]byte(env.Data), pt) {
			t.Fatalf("payload not obfuscated at len=%d", n)
		}
	}
}

func TestObfuscateFrameRoundTrip(t *testing.T) {
	o := NewObfuscator([]byte("k"))
	pt := []byte("the quick brown fox jumps over the lazy dog")
	f := o.Frame(pt)
	// [4-byte len][12 nonce][payload]
	if len(f) != 4+ivLen+len(pt) {
		t.Fatalf("frame length = %d, want %d", len(f), 4+ivLen+len(pt))
	}
	n := int(f[0])<<24 | int(f[1])<<16 | int(f[2])<<8 | int(f[3])
	if n != len(pt) {
		t.Fatalf("frame declared len = %d, want %d", n, len(pt))
	}
	iv := f[4 : 4+ivLen]
	ct := f[4+ivLen:]
	got, err := o.OpenFrame(iv, ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, pt) {
		t.Fatal("frame round trip mismatch")
	}
}

func TestObfuscateWrongSecret(t *testing.T) {
	a := NewObfuscator([]byte("secret-a"))
	b := NewObfuscator([]byte("secret-b"))
	env := a.Seal([]byte("confidential"))
	got, _ := b.Open(env) // no auth tag: succeeds but yields garbage
	if bytes.Equal(got, []byte("confidential")) {
		t.Fatal("wrong secret recovered the plaintext")
	}
}

// TestObfuscateVector pins the keystream so the JS/Python/PowerShell clients
// can be checked against the exact same bytes. secret, nonce and plaintext are
// fixed; the XORed output is deterministic.
func TestObfuscateVector(t *testing.T) {
	o := NewObfuscator([]byte("nullops-test-secret"))
	nonce := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	pt := []byte("hello world")
	out := append([]byte(nil), pt...)
	o.xor(nonce, out)
	got := hex.EncodeToString(out)
	t.Logf("obfuscate vector: secret=%q nonce=%x pt=%q -> %s", o.secret, nonce, pt, got)

	const want = "243f58258653727c088c1b"
	if got != want {
		t.Fatalf("vector mismatch: got %s want %s", got, want)
	}
}
