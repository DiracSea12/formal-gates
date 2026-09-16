//go:build !cgo

package main

// builtWithCgo reports whether this probe binary was built with cgo enabled.
// The plan requires the SQLite driver to work without Cgo, so the acceptance
// runner builds and runs the probe once with CGO_ENABLED=0 as well.
const builtWithCgo = false
