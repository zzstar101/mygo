package mygo

import (
	"os"
	"path/filepath"
	"syscall"
)

// OpenRoot uses an ordinary Windows handle that prevents deletion and rename.
// Root.OpenRoot shares deletion, so retain that anchored handle instead and
// release the temporary parent before returning.
func fileWatchOpenRoot(path string) (*os.Root, error) {
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return parent.OpenRoot(filepath.Base(path))
}

func fileWatchLeaf(info os.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
