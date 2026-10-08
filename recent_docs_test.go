//go:build !js

package mygo

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

// recoverPanic runs fn and returns the string it panicked with, or "".
func recoverPanic(fn func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg, _ = r.(string)
		}
	}()
	fn()
	return ""
}

func TestAddRecentDocument(t *testing.T) {
	onMain(func() { fb.RecentDocuments = nil; fb.RecentCleared = 0 })
	recent := func() []string { return onMainValue(func() []string { return fb.RecentDocuments }) }

	// A relative path is recorded as an absolute one.
	if err := App.AddRecentDocument("notes/todo.txt"); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs("notes/todo.txt")
	if got := recent(); len(got) != 1 || got[0] != want {
		t.Errorf("RecentDocuments = %q, want [%q]", got, want)
	}
	// Entries keep the order they were added in.
	if err := App.AddRecentDocument(want); err != nil {
		t.Fatal(err)
	}
	if got := recent(); len(got) != 2 || got[1] != want {
		t.Errorf("RecentDocuments = %q, want the second to be %q", got, want)
	}
	// An empty path is a programming error.
	if msg := recoverPanic(func() { App.AddRecentDocument("") }); msg != "mygo: App.AddRecentDocument needs a path" {
		t.Errorf("AddRecentDocument(\"\") panicked with %q", msg)
	}
}

func TestClearRecentDocuments(t *testing.T) {
	onMain(func() { fb.RecentDocuments = []string{"/a", "/b"}; fb.RecentCleared = 0 })
	if err := App.ClearRecentDocuments(); err != nil {
		t.Fatal(err)
	}
	if got := onMainValue(func() []string { return fb.RecentDocuments }); got != nil {
		t.Errorf("RecentDocuments after clear = %q, want none", got)
	}
	if n := onMainValue(func() int { return fb.RecentCleared }); n != 1 {
		t.Errorf("RecentCleared = %d, want 1", n)
	}
}

func TestSetJumpList(t *testing.T) {
	onMain(func() { fb.JumpListTasks = nil })
	tasks := []JumpListTask{
		{Title: "New note", Args: "--new"},
		{Title: "Open", Path: "/usr/bin/open", IconPath: "/icon.ico", IconIndex: 3},
	}
	if err := App.SetJumpList(tasks); err != nil {
		t.Fatal(err)
	}
	got := onMainValue(func() []platform.JumpListTask { return fb.JumpListTasks })
	want := []platform.JumpListTask{
		{Title: "New note", Args: "--new"},
		{Title: "Open", Path: "/usr/bin/open", IconPath: "/icon.ico", IconIndex: 3},
	}
	if len(got) != len(want) {
		t.Fatalf("JumpListTasks = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("task %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	// No tasks removes them.
	if err := App.SetJumpList(nil); err != nil {
		t.Fatal(err)
	}
	if got := onMainValue(func() []platform.JumpListTask { return fb.JumpListTasks }); len(got) != 0 {
		t.Errorf("JumpListTasks after SetJumpList(nil) = %+v, want none", got)
	}
	// A task without a title is a programming error.
	if msg := recoverPanic(func() { App.SetJumpList([]JumpListTask{{Args: "--x"}}) }); msg != "mygo: App.SetJumpList: every task needs a title" {
		t.Errorf("SetJumpList with an untitled task panicked with %q", msg)
	}
}
