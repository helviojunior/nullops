package server

import (
	"archive/zip"
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/helviojunior/nullops/pkg/auth"
	"github.com/helviojunior/nullops/pkg/crypto"
	"github.com/helviojunior/nullops/pkg/fileops"
	"github.com/helviojunior/nullops/pkg/pads"
	"github.com/helviojunior/nullops/pkg/session"
)

type env struct {
	t     *testing.T
	srv   *httptest.Server
	users *auth.Store
	store *session.SQLiteStore
	root  string
}

func newEnv(t *testing.T, mutate func(*Config)) *env {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	os.MkdirAll(root, 0o755)
	users, err := auth.Open(filepath.Join(dir, "users.json"))
	if err != nil {
		t.Fatal(err)
	}
	users.Set("alice", "alicepass1", false, false)
	users.Set("reader", "readerpass1", true, false)
	st, err := session.OpenSQLite(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	fsys, _ := fileops.New(root, fileops.Options{})
	padStore, err := pads.OpenStore(st.DB(), filepath.Join(dir, "pads"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Users: users, FS: fsys, Sessions: st, SessionTTL: time.Hour, Title: "test",
		Pads: pads.NewManager(padStore)}
	if mutate != nil {
		mutate(&cfg)
	}
	s := newServer(cfg)
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return &env{t: t, srv: ts, users: users, store: st, root: root}
}

// client is a minimal Go port of frontend/src/lib/{crypto,api}.js.
type client struct {
	e   *env
	sid string
	c   *crypto.Cipher
}

func (e *env) connect() *client {
	e.t.Helper()
	priv, _ := ecdh.P256().GenerateKey(rand.Reader)
	body, _ := json.Marshal(map[string]string{"client_pub": crypto.B64.EncodeToString(priv.PublicKey().Bytes())})
	resp, err := http.Post(e.srv.URL+"/api/handshake", "application/json", bytes.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var hs struct {
		SessionID string `json:"session_id"`
		ServerPub string `json:"server_pub"`
	}
	json.NewDecoder(resp.Body).Decode(&hs)
	raw, _ := crypto.B64.DecodeString(hs.ServerPub)
	peer, err := ecdh.P256().NewPublicKey(raw)
	if err != nil {
		e.t.Fatal("bad server key", err)
	}
	shared, _ := priv.ECDH(peer)
	salt, _ := hex.DecodeString(hs.SessionID)
	key, _ := hkdf.Key(sha256.New, shared, salt, crypto.HKDFInfo, 32)
	c, _ := crypto.NewCipher(key)
	return &client{e: e, sid: hs.SessionID, c: c}
}

func (c *client) raw(action string, payload any) *http.Response {
	b, _ := json.Marshal(payload)
	env := c.c.Seal(b)
	body, _ := json.Marshal(env)
	req, _ := http.NewRequest("POST", c.e.srv.URL+"/api/"+action, bytes.NewReader(body))
	req.Header.Set("X-Session-Id", c.sid)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	return resp
}

func (c *client) call(action string, payload any) map[string]any {
	c.e.t.Helper()
	resp := c.raw(action, payload)
	defer resp.Body.Close()
	var envl crypto.Envelope
	data, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(data, &envl); err != nil || envl.IV == "" {
		var plain map[string]any
		json.Unmarshal(data, &plain)
		plain["_status"] = float64(resp.StatusCode)
		return plain
	}
	pt, err := c.c.Open(envl)
	if err != nil {
		c.e.t.Fatal("decrypt:", err)
	}
	var out map[string]any
	json.Unmarshal(pt, &out)
	return out
}

func (c *client) login(user, pass string) map[string]any {
	c.e.t.Helper()
	r := c.call("login_start", map[string]string{"username": user})
	if r["next"] != "password" {
		c.e.t.Fatalf("login_start %s: %v", user, r)
	}
	return c.call("login", map[string]string{"username": user, "password": pass})
}

func TestLoginPhasesAndDeception(t *testing.T) {
	e := newEnv(t, nil)
	c := e.connect()

	// no login started: the password step is refused
	if r := c.call("login", map[string]string{"username": "alice", "password": "alicepass1"}); r["ok"] != false {
		t.Fatalf("password without login_start: %v", r)
	}
	// unknown user looks like an existing MFA user, and its MFA always fails
	if r := c.call("login_start", map[string]string{"username": "ghost"}); r["next"] != "mfa" {
		t.Fatalf("unknown user must ask for MFA: %v", r)
	}
	if r := c.call("login_mfa", map[string]string{"code": "123456"}); r["ok"] != false {
		t.Fatalf("unknown user MFA accepted: %v", r)
	}
	if r := c.call("login", map[string]string{"password": "x"}); r["ok"] != false {
		t.Fatalf("unknown user reached the password: %v", r)
	}
	// existing user without MFA: user -> password
	if r := c.login("alice", "alicepass1"); r["ok"] != true {
		t.Fatalf("login: %v", r)
	}
	if r := c.call("info", map[string]any{}); r["ok"] != true {
		t.Fatalf("info after login: %v", r)
	}
}

func TestMFALogin(t *testing.T) {
	e := newEnv(t, nil)
	secret, _ := auth.NewTOTPSecret()
	e.users.SetMFA("alice", secret)
	c := e.connect()
	if r := c.call("login_start", map[string]string{"username": "alice"}); r["next"] != "mfa" {
		t.Fatalf("mfa user: %v", r)
	}
	// the password step is locked until the code is verified
	if r := c.call("login", map[string]string{"username": "alice", "password": "alicepass1"}); r["ok"] != false {
		t.Fatalf("password before MFA: %v", r)
	}
	if r := c.call("login_mfa", map[string]string{"code": "000000"}); r["ok"] != false {
		t.Fatalf("wrong code accepted: %v", r)
	}
	code, _ := auth.TOTPCode(secret, time.Now())
	if r := c.call("login_mfa", map[string]string{"code": code}); r["next"] != "password" {
		t.Fatalf("valid code: %v", r)
	}
	if r := c.call("login", map[string]string{"username": "alice", "password": "alicepass1"}); r["ok"] != true || r["mfa"] != true {
		t.Fatalf("login: %v", r)
	}
	// the same code cannot be replayed
	c2 := e.connect()
	c2.call("login_start", map[string]string{"username": "alice"})
	if r := c2.call("login_mfa", map[string]string{"code": code}); r["ok"] != false {
		t.Fatalf("replayed code accepted: %v", r)
	}
}

func TestMFAEnrollFromUI(t *testing.T) {
	e := newEnv(t, nil)
	c := e.connect()
	c.login("alice", "alicepass1")
	setup := c.call("mfa_setup", map[string]any{})["result"].(map[string]any)
	secret := setup["secret"].(string)
	if r := c.call("mfa_enable", map[string]string{"code": "111111"}); r["ok"] != false {
		t.Fatal("wrong enrollment code accepted")
	}
	code, _ := auth.TOTPCode(secret, time.Now())
	if r := c.call("mfa_enable", map[string]string{"code": code}); r["ok"] != true {
		t.Fatalf("enable: %v", r)
	}
	if u, _ := e.users.Get("alice"); !u.HasMFA() {
		t.Fatal("MFA not stored")
	}
}

func TestBruteForceLockout(t *testing.T) {
	e := newEnv(t, nil)
	c := e.connect()
	for i := 0; i < maxFails; i++ {
		c.call("login_start", map[string]string{"username": "alice"})
		c.call("login", map[string]string{"password": "wrong"})
	}
	c.call("login_start", map[string]string{"username": "alice"})
	if r := c.call("login", map[string]string{"password": "alicepass1"}); r["error"] != "Too many attempts" {
		t.Fatalf("not locked out: %v", r)
	}
}

func TestAuthorizationAndConfinement(t *testing.T) {
	e := newEnv(t, nil)
	os.WriteFile(filepath.Join(filepath.Dir(e.root), "secret.txt"), []byte("x"), 0o644)

	anon := e.connect()
	if r := anon.call("list", map[string]string{"path": ""}); r["_status"] != float64(401) {
		t.Fatalf("unauthenticated list: %v", r)
	}
	ro := e.connect()
	ro.login("reader", "readerpass1")
	if r := ro.call("mkdir", map[string]string{"path": "", "name": "x"}); r["error"] != "Read-only mode" {
		t.Fatalf("read-only mkdir: %v", r)
	}
	c := e.connect()
	c.login("alice", "alicepass1")
	if r := c.call("read", map[string]string{"path": "../secret.txt"}); r["ok"] != false {
		t.Fatalf("read outside root: %v", r)
	}
}

func TestSessionRevocation(t *testing.T) {
	session.SetRevocationCheck(0)
	defer session.SetRevocationCheck(5 * time.Second)
	e := newEnv(t, nil)
	c := e.connect()
	c.login("alice", "alicepass1")
	if r := c.call("list", map[string]string{"path": ""}); r["ok"] != true {
		t.Fatal(r)
	}
	time.Sleep(10 * time.Millisecond)
	e.store.DeleteUser("alice") // `nullops sessions revoke --user alice`
	if r := c.call("list", map[string]string{"path": ""}); r["_status"] != float64(401) {
		t.Fatalf("revoked session still works: %v", r)
	}
}

func TestZipDownloadAndConflict(t *testing.T) {
	e := newEnv(t, nil)
	os.MkdirAll(filepath.Join(e.root, "d"), 0o755)
	os.WriteFile(filepath.Join(e.root, "d", "a.txt"), []byte("hello"), 0o644)
	c := e.connect()
	c.login("alice", "alicepass1")

	resp := c.raw("download", map[string]any{"path": "d"})
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var plain []byte
	for len(data) >= 16 {
		n := int(binary.BigEndian.Uint32(data[:4]))
		pt, err := c.c.OpenFrame(data[4:16], data[16:16+n])
		if err != nil {
			t.Fatal(err)
		}
		plain = append(plain, pt...)
		data = data[16+n:]
	}
	zr, err := zip.NewReader(bytes.NewReader(plain), int64(len(plain)))
	if err != nil || len(zr.File) < 2 {
		t.Fatalf("zip: %v %d", err, len(plain))
	}

	doc := c.call("read", map[string]string{"path": "d/a.txt"})["result"].(map[string]any)
	os.WriteFile(filepath.Join(e.root, "d", "a.txt"), []byte("changed elsewhere"), 0o644)
	r := c.call("save", map[string]any{"path": "d/a.txt", "content": "mine", "encoding": doc["encoding"],
		"eol": doc["eol"], "hash": doc["hash"]})
	if r["code"] != "conflict" {
		t.Fatalf("conflict not reported: %v", r)
	}
}

func TestTrustedProxy(t *testing.T) {
	nets, _ := ParseTrustedProxies([]string{"10.0.0.0/8"})
	s := &Server{cfg: Config{TrustedProxies: nets}}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.1.2.3:5555"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.9, 10.9.9.9")
	if ip := s.clientIP(r); ip != "203.0.113.9" {
		t.Fatalf("got %s", ip)
	}
	r.RemoteAddr = "198.51.100.7:1"
	if ip := s.clientIP(r); ip != "198.51.100.7" {
		t.Fatalf("untrusted proxy honoured: %s", ip)
	}
}

func TestPastePadOverHTTP(t *testing.T) {
	e := newEnv(t, nil)
	a := e.connect()
	a.login("alice", "alicepass1")
	b := e.connect()
	b.login("reader", "readerpass1")

	if r := a.call("pad_create", map[string]string{"name": "bad name!"}); r["ok"] != false {
		t.Fatalf("invalid name accepted: %v", r)
	}
	if r := a.call("pad_create", map[string]string{"name": "team.notes-1"}); r["ok"] != true {
		t.Fatal(r)
	}
	// alice types, bob (another session/user) receives it through the long poll
	done := make(chan map[string]any, 1)
	go func() {
		done <- b.call("pad_poll", map[string]any{"name": "team.notes-1", "client": "B", "rev": 0})
	}()
	time.Sleep(100 * time.Millisecond)
	r := a.call("pad_edit", map[string]any{"name": "team.notes-1", "client": "A", "seq": 1, "rev": 0, "op": []any{"hello"}})
	if r["ok"] != true {
		t.Fatal(r)
	}
	select {
	case p := <-done:
		res := p["result"].(map[string]any)
		ch := res["changes"].([]any)
		if res["rev"] != float64(1) || len(ch) != 1 {
			t.Fatalf("poll: %v", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("long poll did not return")
	}
	// concurrent edits on revision 1 are merged
	a.call("pad_edit", map[string]any{"name": "team.notes-1", "client": "A", "seq": 2, "rev": 1, "op": []any{5, " world"}})
	b.call("pad_edit", map[string]any{"name": "team.notes-1", "client": "B", "seq": 1, "rev": 1, "op": []any{">> ", 5}})
	snap := b.call("pad_open", map[string]any{"name": "team.notes-1", "client": "B"})["result"].(map[string]any)
	if snap["text"] != ">> hello world" {
		t.Fatalf("merged text: %v", snap["text"])
	}
	list := a.call("pad_list", map[string]any{})["result"].([]any)
	if len(list) != 1 {
		t.Fatalf("list: %v", list)
	}
	// anonymous sessions cannot reach pads
	if r := e.connect().call("pad_list", map[string]any{}); r["_status"] != float64(401) {
		t.Fatalf("unauthenticated pad access: %v", r)
	}
}
