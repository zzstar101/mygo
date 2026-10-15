//go:build !mygo_noinspector

package ui

// reloadable tells that this build keeps the state of its elements across
// a reload of mygo dev (reload.go). Production builds, which leave the
// developer tools out (reload_off.go), leave it out too.
const reloadable = true
