// Package datadir resolves the on-disk location of the user's state: TLS
// certificate/key, the user database and the sessions. Everything lives under
// a single directory, ~/.nullops by default (%USERPROFILE%\.nullops on Windows).
package datadir

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Default is the default data directory: ".nullops" in the user's home.
const Default = "~/.nullops"

// Dir is a resolved data directory.
type Dir struct {
	Root string
}

// Resolve returns the absolute data directory for path (Default when empty).
func Resolve(path string) (*Dir, error) {
	if path == "" {
		path = Default
	}
	path, err := expandHome(path)
	if err != nil {
		return nil, err
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
				"(use --data-dir to choose another folder)", d.Root)
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

// SessionsFile is the SQLite database of authenticated sessions.
func (d *Dir) SessionsFile() string { return filepath.Join(d.Root, "sessions.db") }

// expandHome turns a leading "~" into the user's home directory.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find the home directory (use --data-dir): %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}
