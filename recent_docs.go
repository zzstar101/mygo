package mygo

import (
	"errors"
	"path/filepath"

	"github.com/egoist/mygo/internal/platform"
)

// AddRecentDocument records path as a document the user opened, for the
// system to offer it again: in the app's Dock menu on macOS, in the
// recently used files of the user (the file chooser's and file manager's
// Recent) on Linux, and in the Recent category of the app's jump list on
// Windows. Call it whenever the app opens a document, including files the
// user chose with a file dialog:
//
//	if err := mygo.App.AddRecentDocument(path); err != nil { … }
//
// A relative path is resolved against the working directory. Choosing an
// entry of the Dock menu or the jump list opens the file with the app,
// which gets it through OnOpenFile.
func (a *Application) AddRecentDocument(path string) error {
	needsApp("App.AddRecentDocument")
	if path == "" {
		return errors.New("mygo: App.AddRecentDocument needs a path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return onMainValue(func() error { return backend().App().AddRecentDocument(abs) })
}

// ClearRecentDocuments removes the documents the app recorded with
// AddRecentDocument: the app's recent documents on macOS, the files the app
// recorded on Linux (the user's other recent files stay), and the Recent
// category of the app's jump list on Windows.
func (a *Application) ClearRecentDocuments() error {
	needsApp("App.ClearRecentDocuments")
	return onMainValue(func() error { return backend().App().ClearRecentDocuments() })
}

// JumpListTask is an entry of the Tasks category of the app's jump list on
// Windows, the menu of its taskbar button. It runs a program, the app
// itself by default, whether the app is running or not.
type JumpListTask struct {
	// Title is the entry's label. It is required.
	Title string
	// Path is the program to run; empty is the app's own executable.
	Path string
	// Args is the command line passed to the program, as one string.
	Args string
	// IconPath and IconIndex choose the icon: the file holding it and the
	// zero-based index of the icon in that file. An empty IconPath uses the
	// program's own icon.
	IconPath  string
	IconIndex int
}

// SetJumpListTasks sets the Tasks category of the app's jump list on
// Windows, like Electron's app.setUserTasks; no tasks removes them. Windows
// keeps the list after the app quits, so apps usually set it once at
// startup:
//
//	if err := mygo.App.SetJumpListTasks([]mygo.JumpListTask{
//		{Title: "New note", Args: "--new"},
//	}); err != nil { … }
//
// A task that runs the app starts another instance with Args; with
// App.RequestSingleInstanceLock, the first instance gets them through
// OnSecondInstance. SetJumpListTasks does nothing on macOS and Linux.
func (a *Application) SetJumpListTasks(tasks []JumpListTask) error {
	needsApp("App.SetJumpListTasks")
	list := make([]platform.JumpListTask, len(tasks))
	for i, t := range tasks {
		if t.Title == "" {
			return errors.New("mygo: App.SetJumpListTasks: every task needs a title")
		}
		list[i] = platform.JumpListTask(t)
	}
	return onMainValue(func() error { return backend().App().SetJumpListTasks(list) })
}
