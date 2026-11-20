// Package version is the build's version: a release stamps it, and
// /healthz shows it (docs/adr/0014-production-topology.md).
package version

// Version is "dev" unless the build sets it, as the images do:
//
//	go build -ldflags "-X github.com/rokumate/ronote/internal/version.Version=v1.2.3"
var Version = "dev"
