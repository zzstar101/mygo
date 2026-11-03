//go:build !unix

package hotlink

import "os"

func mapFile(file string) ([]byte, error) { return os.ReadFile(file) }
