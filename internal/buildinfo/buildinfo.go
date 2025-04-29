// Package buildinfo provides version and build information for the Void application.
// It exposes variables that are set at link-time to identify the version and
// commit hash of the build.
package buildinfo

// Version is set at link-time with –ldflags.
// Default is “dev” so tests and “go run .” still work.
var Version = "dev"

// Commit is set at link-time with –ldflags.
// Default is “unknown” so tests and “go run .” still work.
var Commit = "unknown"
