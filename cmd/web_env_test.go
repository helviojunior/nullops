package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func newEnvTestCmd() (*cobra.Command, *struct {
	root, bind string
	port       int
	noTLS      bool
	ext        []string
}) {
	v := &struct {
		root, bind string
		port       int
		noTLS      bool
		ext        []string
	}{}
	cmd := &cobra.Command{Use: "web"}
	f := cmd.Flags()
	f.StringVarP(&v.root, "root", "r", ".", "")
	f.StringVarP(&v.bind, "bind", "b", "127.0.0.1", "")
	f.IntVarP(&v.port, "port", "p", 8181, "")
	f.BoolVar(&v.noTLS, "no-tls", false, "")
	f.StringSliceVar(&v.ext, "allow-ext", nil, "")
	return cmd, v
}

func TestApplyEnvFlags(t *testing.T) {
	cmd, v := newEnvTestCmd()
	// --port given on the command line: the env value must not override it.
	if err := cmd.Flags().Set("port", "9000"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NULLOPS_ROOT", "/data")
	t.Setenv("NULLOPS_BIND", "0.0.0.0")
	t.Setenv("NULLOPS_PORT", "7000") // ignored: command line wins
	t.Setenv("NULLOPS_NO_TLS", "true")
	t.Setenv("NULLOPS_ALLOW_EXT", "pdf,txt")
	t.Setenv("NULLOPS_TITLE", "") // empty is treated as unset, no flag exists anyway

	if err := applyEnvFlags(cmd); err != nil {
		t.Fatalf("applyEnvFlags: %v", err)
	}
	if v.root != "/data" {
		t.Errorf("root = %q, want /data", v.root)
	}
	if v.bind != "0.0.0.0" {
		t.Errorf("bind = %q, want 0.0.0.0", v.bind)
	}
	if v.port != 9000 {
		t.Errorf("port = %d, want 9000 (command line wins)", v.port)
	}
	if !v.noTLS {
		t.Error("no-tls = false, want true")
	}
	if len(v.ext) != 2 || v.ext[0] != "pdf" || v.ext[1] != "txt" {
		t.Errorf("allow-ext = %v, want [pdf txt]", v.ext)
	}
}

func TestApplyEnvFlagsUnsetAndInvalid(t *testing.T) {
	// No env vars set: nothing changes, defaults stay.
	cmd, v := newEnvTestCmd()
	if err := applyEnvFlags(cmd); err != nil {
		t.Fatal(err)
	}
	if v.root != "." || v.port != 8181 || v.noTLS {
		t.Fatalf("defaults changed without env: %+v", v)
	}

	// An unparseable boolean is reported, not silently ignored.
	cmd2, _ := newEnvTestCmd()
	t.Setenv("NULLOPS_NO_TLS", "maybe")
	if err := applyEnvFlags(cmd2); err == nil {
		t.Fatal("expected an error for NULLOPS_NO_TLS=maybe")
	}
}
