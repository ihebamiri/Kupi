//go:build production

// Package buildmode reports whether this is a production build.
package buildmode

// Production is true in release builds (built with -tags production).
const Production = true
