package mygo

import (
	"path/filepath"

	"github.com/egoist/mygo/internal/platform"
)

// AddRecentDocument records path as a document the app recently opened, so
// that the system shows it where the user picks documents from: the app's
// Dock menu (macOS), the recent files of the app's desktop entry (Linux) and
// the Recent category of the app's jump list (Windows). Choosing one of
// those entries delivers the path to App.OnOpenFile, as opening the file
// from the file manager does.
//
// Call it whenever the app opens a document, including files the user chose
// with a file dialog:
//
//	if err := mygo.App.AddRecentDocument(path); err != nil { … }
//
// Relative paths resolve against the process's working directory. The path
// does not have to exist: the systems keep entries for files that were
// moved or removed.
//
// It returns platform.ErrUnsupported on platforms without a desktop shell.
func (a *Application) AddRecentDocument(path string) error {
	needsApp("App.AddRecentDocument")
	if path == "" {
		panic("mygo: App.AddRecentDocument needs a path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return onMainValue(func() error { return backend().App().AddRecentDocument(abs) })
}

// ClearRecentDocuments removes the recent documents the app added with
// AddRecentDocument: the Open Recent menu on macOS, the app's entries in the
// shared recent files list on Linux (other applications' entries stay) and
// the usage data the shell tracks for the app on Windows, which also empties
// the Recent category of its jump list.
//
//	if err := mygo.App.ClearRecentDocuments(); err != nil { … }
//
// It returns platform.ErrUnsupported on platforms without a desktop shell.
func (a *Application) ClearRecentDocuments() error {
	needsApp("App.ClearRecentDocuments")
	return onMainValue(func() error { return backend().App().ClearRecentDocuments() })
}

// JumpListTask is an entry of the Tasks category of the Windows jump list,
// the menu that appears when the user right-clicks the app's taskbar
// button. A task runs a command.
type JumpListTask struct {
	// Title is the label shown in the jump list. It is required.
	Title string
	// Path is the program the task runs. An empty value is the app's own
	// executable.
	Path string
	// Args is the command line passed to the program, as one string.
	Args string
	// IconPath and IconIndex choose the icon: the file holding it and the
	// zero-based index of the icon in that file. An empty IconPath uses
	// the program's own icon.
	IconPath  string
	IconIndex int
}

// SetJumpList sets the tasks of the app's jump list (Windows), like
// Electron's app.setUserTasks. Tasks must work when the app is not running,
// and should be static: Windows shows the last list the app committed, so
// apps set them once, usually at startup. Calling it with no tasks removes
// the app's tasks.
//
//	if err := mygo.App.SetJumpList([]mygo.JumpListTask{
//		{Title: "New note", Args: "--new"},
//	}); err != nil { … }
//
// The Recent category of the jump list is filled by the system from
// AddRecentDocument; for its items to appear the app must be a registered
// handler of the file type (see fileAssociations in the CLI guide).
//
// SetJumpList does nothing on macOS and Linux, which have no jump list.
func (a *Application) SetJumpList(tasks []JumpListTask) error {
	needsApp("App.SetJumpList")
	for _, t := range tasks {
		if t.Title == "" {
			panic("mygo: App.SetJumpList: every task needs a title")
		}
	}
	list := make([]platform.JumpListTask, len(tasks))
	for i, t := range tasks {
		list[i] = platform.JumpListTask(t)
	}
	return onMainValue(func() error { return backend().App().SetJumpList(list) })
}
