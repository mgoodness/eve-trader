// Package build holds build-time identifiers injected via -ldflags -X
// (see .goreleaser.yaml's ldflags). "Version" in this project means the
// git commit it was built from, not a semver string -- ADR-0008 dropped
// version tags and GitHub Releases entirely in favor of continuous
// deploy, so there's no semver to track.
package build

// Commit is the short git commit SHA this binary was built from,
// injected at build time via -ldflags -X. It defaults to "dev" -- a
// clear placeholder, not a blank string -- for a plain go build/go run
// that skips that injection.
var Commit = "dev"
