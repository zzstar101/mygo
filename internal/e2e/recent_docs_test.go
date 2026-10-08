package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/egoist/mygo"
)

// TestRecentDocuments records a document as recently opened, checks the
// system kept the entry, then clears the recent documents and checks it
// dropped it. Choosing such an entry would deliver the path to
// App.OnOpenFile, as opening the file from the file manager does.
func TestRecentDocuments(t *testing.T) {
	if !recentDocumentsSupported() {
		t.Skip("no desktop shell keeps recent documents on this platform")
	}
	// On Linux the recent files list is shared and per-user: keep the test's
	// entries out of the runner's own, as TestURLScheme does for its handlers.
	if runtime.GOOS == "linux" && os.Getenv("XDG_DATA_HOME") == "" {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	}
	path := filepath.Join(t.TempDir(), "recent document.txt")
	if err := os.WriteFile(path, []byte("recent"), 0o644); err != nil {
		t.Fatal(err)
	}
	recorded := func() bool { r, _ := recentDocumentRecorded(path); return r }
	defer func() { mygo.App.ClearRecentDocuments() }()

	if err := mygo.App.AddRecentDocument(path); err != nil {
		t.Fatalf("AddRecentDocument: %v", err)
	}
	eventually(t, "the document to be recorded as recent", recorded)

	if err := mygo.App.ClearRecentDocuments(); err != nil {
		t.Fatalf("ClearRecentDocuments: %v", err)
	}
	eventually(t, "the recent documents to be cleared", func() bool { return !recorded() })
}

// TestJumpList commits the tasks of the jump list. Only Windows has one;
// there, committing must succeed. Elsewhere SetJumpList is a no-op that
// returns no error.
func TestJumpList(t *testing.T) {
	tasks := []mygo.JumpListTask{
		{Title: "New window", Args: "--new"},
		{Title: "No arguments"},
	}
	if err := mygo.App.SetJumpList(tasks); err != nil {
		t.Errorf("SetJumpList: %v", err)
	}
	if err := mygo.App.SetJumpList(nil); err != nil {
		t.Errorf("SetJumpList(nil): %v", err)
	}
}
