// Package datadir resolves the on-disk location of the user's state: TLS
// certificate/key and the user database. Everything lives under a single
// directory, ./fmweb by default.
package datadir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Default is the default data directory, relative to the working directory.
const Default = "./fmweb"

// Dir is a resolved data directory.
type Dir struct {
	Root string
}

// Resolve returns the absolute data directory for path (Default when empty).
func Resolve(path string) (*Dir, error) {
	if path == "" {
		path = Default
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return &Dir{Root: abs}, nil
}

// Ensure creates the data directory (0700) when it does not exist yet.
func (d *Dir) Ensure() error {
	st, err := os.Stat(d.Root)
	if err == nil {
		if !st.IsDir() {
			return fmt.Errorf("data directory %q exists but is not a directory "+
				"(is the fmweb binary in the same folder? use --data-dir)", d.Root)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.MkdirAll(d.Root, 0o700)
}

// TLSDir is the folder holding the certificate and private key.
func (d *Dir) TLSDir() string { return filepath.Join(d.Root, "tls") }

// CertFile is the PEM certificate path.
func (d *Dir) CertFile() string { return filepath.Join(d.TLSDir(), "cert.pem") }

// KeyFile is the PEM private key path.
func (d *Dir) KeyFile() string { return filepath.Join(d.TLSDir(), "key.pem") }

// UsersFile is the JSON user database path.
func (d *Dir) UsersFile() string { return filepath.Join(d.Root, "users.json") }
