package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestLivePythonSenderObfuscated runs the real senders/upload.py process over
// HTTP against a live server, forcing the obfuscated mode (HAVE_CRYPTO=False),
// and checks the stored file. Skipped when python3 is unavailable.
func TestLivePythonSenderObfuscated(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	script, err := filepath.Abs("../../senders/upload.py")
	if err != nil || !fileExists(script) {
		t.Skip("senders/upload.py not found")
	}

	e := newEnv(t, func(c *Config) {
		c.Plaintext = true
		c.AllowObfuscated = true
	})
	rq, err := e.cfgForTest.UploadReqs.Create("", "alice")
	if err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(t.TempDir(), "payload.bin")
	content := []byte("live obfuscated upload through the real python sender\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	// Import upload.py as a module, disable the crypto path so it negotiates
	// the obfuscated mode, then call main() with the positional arguments.
	const driver = `
import importlib.util, sys
spec = importlib.util.spec_from_file_location("up", sys.argv[1])
up = importlib.util.module_from_spec(spec)
spec.loader.exec_module(up)
up.HAVE_CRYPTO = False
sys.argv = ["up", sys.argv[2], sys.argv[3], sys.argv[4]]
up.main()
`
	cmd := exec.Command(py, "-c", driver, script, e.srv.URL, rq.Token, src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sender failed: %v\n%s", err, out)
	}

	// The server stores the upload as "<base>-<epoch>.bin" under the link dir.
	matches, _ := filepath.Glob(filepath.Join(e.root, "payload-*.bin"))
	if len(matches) != 1 {
		t.Fatalf("expected one stored file, found %v (sender output: %s)", matches, out)
	}
	stored, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(content) {
		t.Fatalf("stored content mismatch:\n got %q\nwant %q", stored, content)
	}
}

// TestLivePythonSenderEncrypted runs upload.py in its default (encrypted) mode
// to confirm the new accept-list handshake still negotiates AES-GCM. Skipped
// when python3 or its 'cryptography' package is unavailable.
func TestLivePythonSenderEncrypted(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	if out, err := exec.Command(py, "-c", "import cryptography").CombinedOutput(); err != nil {
		t.Skipf("python 'cryptography' not available: %s", out)
	}
	script, err := filepath.Abs("../../senders/upload.py")
	if err != nil || !fileExists(script) {
		t.Skip("senders/upload.py not found")
	}

	e := newEnv(t, nil) // default: encrypted only
	rq, err := e.cfgForTest.UploadReqs.Create("", "alice")
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "secret.bin")
	content := []byte("live AES-GCM upload through the real python sender\n")
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(py, script, e.srv.URL, rq.Token, src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sender failed: %v\n%s", err, out)
	}
	matches, _ := filepath.Glob(filepath.Join(e.root, "secret-*.bin"))
	if len(matches) != 1 {
		t.Fatalf("expected one stored file, found %v (sender output: %s)", matches, out)
	}
	stored, _ := os.ReadFile(matches[0])
	if string(stored) != string(content) {
		t.Fatalf("stored content mismatch:\n got %q\nwant %q", stored, content)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
