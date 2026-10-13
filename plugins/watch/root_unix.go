//go:build !windows

package watch

import "os"

func isReparse(os.FileInfo) bool { return false }
