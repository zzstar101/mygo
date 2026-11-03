//go:build !darwin && !linux && !windows

package hotpatch

import "errors"

var errUnsupported = errors.New("hot reload does not support this platform")

func reserve(at, limit uint64) (uint64, uint64, error) { return 0, 0, errUnsupported }
func protectCode(at, n uint64) error                   { return errUnsupported }
func protectRead(at, n uint64) error                   { return errUnsupported }
func writeJump(from, to uint64) error                  { return errUnsupported }
