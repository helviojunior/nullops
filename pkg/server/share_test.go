package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/helviojunior/nullops/pkg/session"
	"github.com/helviojunior/nullops/pkg/sharelink"
)

// newShareEnv builds a test server with share links enabled on the same DB.
func newShareEnv(t *testing.T) (*env, *sharelink.Store) {
	t.Helper()
	var store *sharelink.Store
	e := newEnv(t, func(cfg *Config) {
		s, err := sharelink.Open(cfg.Sessions.(*session.SQLiteStore).DB())
		if err != nil {
			t.Fatal(err)
		}
		store = s
		cfg.ShareLinks = s
	})
	return e, store
}

func TestShareDownloadAndNotFound(t *testing.T) {
	e, store := newShareEnv(t)

	want := []byte("hello, shared world\n")
	if err := os.WriteFile(filepath.Join(e.root, "note.txt"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := store.Create("note.txt", "note.txt", "alice", 0)
	if err != nil {
		t.Fatal(err)
	}

	// valid id + token: cleartext download of the raw bytes
	resp, err := http.Get(e.srv.URL + shareURL(l.ID, l.Token))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if string(body) != string(want) {
		t.Fatalf("body = %q, want %q", body, want)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd == "" {
		t.Fatal("missing Content-Disposition")
	}

	// each failing combination is an indistinguishable 404
	bad := []string{
		shareURL(l.ID, "00000000000000000000000000000000"), // wrong token
		shareURL(l.ID, ""), // empty token
		"/share/" + l.ID,   // missing token param
		shareURL("11111111-1111-1111-1111-111111111111", l.Token), // unknown id
		shareURL("not-a-uuid", l.Token),                           // malformed id
	}
	for _, u := range bad {
		r, err := http.Get(e.srv.URL + u)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", u, r.StatusCode)
		}
	}

	// revoked -> 404
	if err := store.Revoke(l.ID); err != nil {
		t.Fatal(err)
	}
	r, _ := http.Get(e.srv.URL + shareURL(l.ID, l.Token))
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("revoked: status = %d, want 404", r.StatusCode)
	}
}

func TestShareRangeRequest(t *testing.T) {
	e, store := newShareEnv(t)
	data := []byte("0123456789abcdef")
	os.WriteFile(filepath.Join(e.root, "blob.bin"), data, 0o644)
	l, _ := store.Create("blob.bin", "blob.bin", "alice", 0)

	req, _ := http.NewRequest("GET", e.srv.URL+shareURL(l.ID, l.Token), nil)
	req.Header.Set("Range", "bytes=4-7")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	if string(body) != "4567" {
		t.Fatalf("range body = %q, want %q", body, "4567")
	}
}

func TestContentDisposition(t *testing.T) {
	cases := []struct {
		name, want string
	}{
		// plain ASCII: no redundant filename*
		{"README.md", `attachment; filename="README.md"`},
		// non-ASCII: ASCII fallback + RFC 5987 UTF-8 form
		{"relatório.pdf", `attachment; filename="relat_rio.pdf"; filename*=UTF-8''relat%C3%B3rio.pdf`},
		// ASCII with a quote/backslash: neutralized in the quoted fallback, still no filename*
		{`a"b\c.txt`, `attachment; filename="a_b_c.txt"`},
		// a char url.PathEscape would NOT escape but RFC 5987 requires (=): only in filename*
		{"a=b€.csv", `attachment; filename="a=b_.csv"; filename*=UTF-8''a%3Db%E2%82%AC.csv`},
	}
	for _, c := range cases {
		if got := contentDisposition(c.name); got != c.want {
			t.Errorf("contentDisposition(%q)\n got  %q\n want %q", c.name, got, c.want)
		}
	}
}

func TestShareDisabledIs404(t *testing.T) {
	e := newEnv(t, nil) // ShareLinks nil
	r, err := http.Get(e.srv.URL + "/share/11111111-1111-1111-1111-111111111111?token=00000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", r.StatusCode)
	}
}
