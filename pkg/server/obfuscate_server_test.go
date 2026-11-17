package server

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/helviojunior/nullops/pkg/crypto"
)

// obfClient mirrors a browser that negotiated the obfuscated channel: the
// keystream secret is the session id bytes (cosmetic mode).
type obfClient struct {
	e   *env
	sid string
	o   *crypto.Obfuscator
}

func connectObf(e *env, accept []string) (*obfClient, map[string]any) {
	e.t.Helper()
	body, _ := json.Marshal(map[string]any{"accept": accept})
	resp, err := http.Post(e.srv.URL+"/api/handshake", "application/json", bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var hs map[string]any
	json.NewDecoder(resp.Body).Decode(&hs)
	if resp.StatusCode != 200 {
		return nil, hs
	}
	sid, _ := hs["session_id"].(string)
	sidBytes, _ := hex.DecodeString(sid)
	return &obfClient{e: e, sid: sid, o: crypto.NewObfuscator(sidBytes)}, hs
}

func (c *obfClient) call(action string, payload any) map[string]any {
	c.e.t.Helper()
	b, _ := json.Marshal(payload)
	env := c.o.Seal(b)
	wire, _ := json.Marshal(env)
	req, _ := http.NewRequest("POST", c.e.srv.URL+"/api/"+action, bytes.NewReader(wire))
	req.Header.Set("X-Session-Id", c.sid)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var respEnv crypto.Envelope
	json.NewDecoder(resp.Body).Decode(&respEnv)
	plain, err := c.o.Open(respEnv)
	if err != nil {
		c.e.t.Fatalf("de-obfuscate response: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(plain, &out); err != nil {
		c.e.t.Fatalf("response not JSON: %v (%q)", err, plain)
	}
	return out
}

func TestHandshakeNegotiatesObfuscated(t *testing.T) {
	e := newEnv(t, func(c *Config) {
		c.Plaintext = true
		c.AllowObfuscated = true
		c.NoAuth = true
	})
	cl, hs := connectObf(e, []string{"obfuscated", "cleartext"})
	if cl == nil {
		t.Fatalf("handshake failed: %v", hs)
	}
	if hs["mode"] != "obfuscated" {
		t.Fatalf("mode = %v, want obfuscated", hs["mode"])
	}
	if hs["encrypted"] != false {
		t.Fatalf("encrypted = %v, want false", hs["encrypted"])
	}
	if _, ok := hs["server_pub"]; ok {
		t.Fatal("obfuscated handshake must not carry a server_pub")
	}
	caps, _ := hs["modes"].(map[string]any)
	if caps["obfuscated"] != true || caps["encrypted"] != true {
		t.Fatalf("advertised caps wrong: %v", caps)
	}

	// A full round trip through open()/writeSealed() with the XOR codec.
	out := cl.call("list", map[string]any{"path": ""})
	if out["ok"] != true {
		t.Fatalf("list over obfuscated channel failed: %v", out)
	}
}

func TestHandshakeNoCommonModeWithoutObf(t *testing.T) {
	// A TLS-style server (no obfuscated/cleartext) refuses a browser that can
	// only offer obfuscated.
	e := newEnv(t, nil)
	cl, hs := connectObf(e, []string{"obfuscated", "cleartext"})
	if cl != nil {
		t.Fatal("expected no common mode")
	}
	if hs["code"] != "no_common_mode" {
		t.Fatalf("code = %v, want no_common_mode", hs["code"])
	}
}

func TestPublicUploadOverObfuscated(t *testing.T) {
	e := newEnv(t, func(c *Config) {
		c.Plaintext = true
		c.AllowObfuscated = true
	})
	rq, err := e.cfgForTest.UploadReqs.Create("", "alice")
	if err != nil {
		t.Fatal(err)
	}
	cl, hs := connectObf(e, []string{"obfuscated"})
	if cl == nil {
		t.Fatalf("handshake failed: %v", hs)
	}
	content := []byte("hello over the obfuscated channel")
	fields := map[string]any{"token": rq.Token, "name": "note.txt", "upload_id": "deadbeef", "size": len(content)}

	st := cl.call("public_upload", merge(fields, map[string]any{"status": true}))
	if st["ok"] != true {
		t.Fatalf("status failed: %v", st)
	}
	res := cl.call("public_upload", merge(fields, map[string]any{
		"offset": 0, "last": true, "chunk": crypto.B64.EncodeToString(content),
	}))
	if res["ok"] != true {
		t.Fatalf("upload failed: %v", res)
	}
	// the stored file must match what we sent, proving the obfuscated chunk
	// survived the full server round trip
	got, err := os.ReadFile(filepath.Join(e.root, resultName(res)))
	if err != nil {
		t.Fatalf("stored file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("stored content = %q, want %q", got, content)
	}
}

func merge(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func resultName(res map[string]any) string {
	r, _ := res["result"].(map[string]any)
	n, _ := r["name"].(string)
	return n
}

func TestHandshakeObfNotOfferedByDefault(t *testing.T) {
	// Without --no-tls the server advertises encrypted only.
	e := newEnv(t, nil)
	body, _ := json.Marshal(map[string]any{"accept": []string{"encrypted", "obfuscated"}})
	resp, err := http.Post(e.srv.URL+"/api/handshake", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var hs map[string]any
	json.NewDecoder(resp.Body).Decode(&hs)
	caps, _ := hs["modes"].(map[string]any)
	if caps["obfuscated"] != false || caps["cleartext"] != false {
		t.Fatalf("defaults should not offer fallback modes: %v", caps)
	}
}
