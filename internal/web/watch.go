//go:build js

package web

import "github.com/egoist/mygo/internal/platform"

func (*Backend) NewFileWatch(func(platform.FileWatchNotice)) (platform.FileWatch, error) {
	return nil, platform.ErrUnsupported
}
