package tlsutil

import (
	"context"
	"crypto/ecdsa"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/acme"
)

func validOpts(dir string) ACMEOptions {
	return ACMEOptions{
		Domains:        []string{"files.example.com"},
		CertFile:       filepath.Join(dir, "cert.pem"),
		KeyFile:        filepath.Join(dir, "key.pem"),
		AccountKeyFile: filepath.Join(dir, "acme_account.key"),
	}
}

func TestACMEOptionsValidation(t *testing.T) {
	dir := t.TempDir()
	bad := map[string]func(o *ACMEOptions){
		"no domains":    func(o *ACMEOptions) { o.Domains = nil },
		"blank domains": func(o *ACMEOptions) { o.Domains = []string{" ", ""} },
		"ip":            func(o *ACMEOptions) { o.Domains = []string{"10.0.0.5"} },
		"wildcard":      func(o *ACMEOptions) { o.Domains = []string{"*.example.com"} },
		"single label":  func(o *ACMEOptions) { o.Domains = []string{"localhost"} },
		"bad email":     func(o *ACMEOptions) { o.Email = "nope" },
		"bad directory": func(o *ACMEOptions) { o.DirectoryURL = "ftp://x" },
		"no paths":      func(o *ACMEOptions) { o.KeyFile = "" },
	}
	for name, mut := range bad {
		o := validOpts(dir)
		mut(&o)
		if err := o.normalize(); err == nil {
			t.Errorf("%s: expected error", name)
		}
		// ObtainACME must reject before any network or file activity.
		if err := ObtainACME(context.Background(), o); err == nil {
			t.Errorf("%s: ObtainACME expected error", name)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "acme_account.key")); err == nil {
		t.Error("account key must not be created for invalid options")
	}
}

func TestACMEOptionsDefaults(t *testing.T) {
	o := validOpts(t.TempDir())
	o.Domains = []string{"Files.Example.com.", "files.example.com", "b.example.com"}
	o.Email = "mailto:admin@example.com"
	if err := o.normalize(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(o.Domains, ",") != "files.example.com,b.example.com" {
		t.Errorf("domains = %v", o.Domains)
	}
	if o.Email != "admin@example.com" || o.HTTPAddr != DefaultACMEHTTPAddr || o.DirectoryURL != acme.LetsEncryptURL {
		t.Errorf("defaults = %+v", o)
	}

	s := validOpts(t.TempDir())
	s.Staging = true
	_ = s.normalize()
	if s.DirectoryURL != LetsEncryptStagingURL {
		t.Errorf("staging directory = %s", s.DirectoryURL)
	}
	c := validOpts(t.TempDir())
	c.Staging = true
	c.DirectoryURL = "https://ca.internal/directory"
	_ = c.normalize()
	if c.DirectoryURL != "https://ca.internal/directory" {
		t.Errorf("custom directory overridden: %s", c.DirectoryURL)
	}
}

func TestAccountKeyPersisted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "tls", "acme_account.key")
	k1, err := loadOrCreateAccountKey(p)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", st.Mode().Perm())
	}
	k2, err := loadOrCreateAccountKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if !k1.Public().(*ecdsa.PublicKey).Equal(k2.Public()) {
		t.Error("account key was not reused")
	}
}

func TestChallengeHandler(t *testing.T) {
	tok := &challengeTokens{m: map[string]string{}}
	tok.set("abc", "abc.thumb")
	h := tok.handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/abc", nil))
	if rec.Code != 200 || rec.Body.String() != "abc.thumb" {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}
	tok.del("abc")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/abc", nil))
	if rec.Code != 404 {
		t.Errorf("deleted token: got %d", rec.Code)
	}
}

func TestPortInUse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	dir := t.TempDir()
	o := validOpts(dir)
	o.HTTPAddr = ln.Addr().String()
	err = ObtainACME(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Errorf("err = %v", err)
	}
	if Exists(o.CertFile, o.KeyFile) {
		t.Error("cert/key must not be written on failure")
	}
}
