//go:build !windows

package mygo

import "os"

func fileWatchLeaf(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 }
