package platform

import (
	"errors"
	"io/fs"
)

// FileWatchTarget identifies an entry selected by core policy. IDs are never reused.
type FileWatchTarget struct {
	ID        uint64
	Path      string
	Directory bool
	Info      fs.FileInfo
}

// FileWatchKind describes a native invalidation hint.
type FileWatchKind uint8

const (
	FileWatchDirty FileWatchKind = iota
	FileWatchWrite
	FileWatchCreate
	FileWatchRemove
	FileWatchMoveFrom
	FileWatchMoveTo
	FileWatchLost
)

// FileWatchNotice invalidates a target or one immediate child.
type FileWatchNotice struct {
	Target uint64
	Kind   FileWatchKind
	Name   string
	Cookie uint64
	Err    error
}

// ErrFileWatchOverflow reports native stream loss.
var ErrFileWatchOverflow = errors.New("file watch native queue overflow")

// FileWatch methods and callbacks run on main; cleanup never requires dispatch.
type FileWatch interface {
	Add(target FileWatchTarget, done func(error))
	Remove(id uint64)
	Close() <-chan struct{}
}
