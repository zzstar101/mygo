//go:build mygo_noinspector

package ui

// Production builds (mygo build, as for the inspector) are never run by
// mygo dev: what package ui keeps across its reloads is left out, so that
// it takes no room in the binary.
const reloadable = false
