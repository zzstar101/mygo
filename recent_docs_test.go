//go:build !js

package mygo

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

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
	// An empty path is not recorded.
	if err := App.AddRecentDocument(""); err == nil {
		t.Error("AddRecentDocument(\"\") succeeded")
	}
	if got := recent(); len(got) != 2 {
		t.Errorf("RecentDocuments = %q after an empty path, want 2 entries", got)
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

func TestSetJumpListTasks(t *testing.T) {
	onMain(func() { fb.JumpListTasks = nil })
	tasks := []JumpListTask{
		{Title: "New note", Args: "--new"},
		{Title: "Open", Path: "/usr/bin/open", IconPath: "/icon.ico", IconIndex: 3},
	}
	if err := App.SetJumpListTasks(tasks); err != nil {
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
	if err := App.SetJumpListTasks(nil); err != nil {
		t.Fatal(err)
	}
	if got := onMainValue(func() []platform.JumpListTask { return fb.JumpListTasks }); len(got) != 0 {
		t.Errorf("JumpListTasks after SetJumpListTasks(nil) = %+v, want none", got)
	}
	// A task without a title sets nothing.
	if err := App.SetJumpListTasks([]JumpListTask{{Title: "A"}, {Args: "--x"}}); err == nil {
		t.Error("SetJumpListTasks with an untitled task succeeded")
	}
	if got := onMainValue(func() []platform.JumpListTask { return fb.JumpListTasks }); len(got) != 0 {
		t.Errorf("JumpListTasks after an untitled task = %+v, want none", got)
	}
}
