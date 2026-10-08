package unsupported

import "github.com/egoist/mygo/internal/platform"

func (*Backend) NewFileWatch(func(func()) bool, func(platform.FileWatchNotice)) (platform.FileWatch, error) {
	return nil, platform.ErrUnsupported
}
