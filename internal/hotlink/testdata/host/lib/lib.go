// Package lib is a package the tests' host imports, which edits change too.
package lib

// Version is compiled into those that use it.
const Version = "lib v1"

// Name says who is running.
func Name() string { return "name v1" }
