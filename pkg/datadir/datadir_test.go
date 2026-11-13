package datadir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDefaultIsInHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	d, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if d.Root != filepath.Join(home, ".nullops") {
		t.Fatalf("got %s", d.Root)
	}
	if err := d.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(d.Root)
	if err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
		t.Fatalf("dir: %v %v", st.Mode(), err)
	}
	if d.UsersFile() != filepath.Join(home, ".nullops", "users.json") {
		t.Fatal(d.UsersFile())
	}
	custom, _ := Resolve("~/other")
	if custom.Root != filepath.Join(home, "other") {
		t.Fatal(custom.Root)
	}
	rel, _ := Resolve("./x")
	if !filepath.IsAbs(rel.Root) {
		t.Fatal("relative path not made absolute")
	}
}
