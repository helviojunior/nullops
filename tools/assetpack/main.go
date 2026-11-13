// Command assetpack seals the built front-end (frontend/dist) into the
// obfuscated blob embedded by pkg/assets. It is run by the Makefile after the
// front-end build and before `go build`.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/helviojunior/fmweb/pkg/assets/bundle"
)

func main() {
	in := flag.String("in", "frontend/dist", "front-end build directory")
	out := flag.String("out", "pkg/assets/bundle.bin", "output blob")
	flag.Parse()

	if _, err := os.Stat(filepath.Join(*in, "index.html")); err != nil {
		fmt.Fprintf(os.Stderr, "assetpack: %s/index.html not found; build the front-end first (make frontend)\n", *in)
		os.Exit(1)
	}
	blob, n, err := bundle.PackDir(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "assetpack:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, blob, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "assetpack:", err)
		os.Exit(1)
	}
	fmt.Printf("assetpack: sealed %d files (%d bytes) into %s\n", n, len(blob), *out)
}
