// Package version contains build information for nullops. The values are
// injected at build time through -ldflags (see the Makefile).
package version

var (
	// Version contains the current version of nullops.
	Version = "dev"
	// GitHash contains the git commit hash at build time.
	GitHash = "dev"
	// GoBuildEnv contains the Go build environment information.
	GoBuildEnv = "dev"
	// GoBuildTime contains the build timestamp.
	GoBuildTime = "dev"
)
