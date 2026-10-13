package mygo

import (
	"encoding/json/v2"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// hasDocuments reports whether the app made documents (NewDocuments),
// which the File menu then has commands for.
func hasDocuments() bool { return defaultDocKind() != nil }

// documentRole performs a document's menu role, from the menu of win.
func documentRole(role MenuRole, win *Window) {
	k := defaultDocKind()
	if k == nil {
		return
	}
	switch role {
	case RoleNew:
		go k.newUntitled()
		return
	case RoleOpen:
		go openDocumentsDialog()
		return
	}
	b := documentFor(win)
	if b == nil {
		b = documentFor(FocusedWindow())
	}
	if b == nil {
		return
	}
	go func() {
		var err error
		switch role {
		case RoleSave:
			err = b.saveInteractive(false)
		case RoleSaveAs:
			err = b.saveInteractive(true)
		case RoleDuplicate:
			b.duplicate()
		case RoleRename:
			err = b.rename("")
		case RoleMoveTo:
			err = b.moveTo("")
		case RoleRevert:
			err = b.revert()
		}
		if err != nil && !errors.Is(err, ErrCanceled) {
			what := map[MenuRole]string{RoleRename: "renamed", RoleMoveTo: "moved", RoleRevert: "reverted"}[role]
			if what == "" {
				what = "saved"
			}
			showDocumentError(b.parent(), "The document “"+b.Name()+"” could not be "+what+".", err)
		}
	}()
}

// openDocumentsDialog asks which documents to open, of every kind.
func openDocumentsDialog() {
	var filters []FileFilter
	for _, k := range docKinds() {
		filters = append(filters, k.filters()...)
	}
	paths, err := Dialog.Open(OpenDialogOptions{Parent: FocusedWindow(), Filters: filters, Multiple: true})
	if err != nil {
		return
	}
	for _, p := range paths {
		openRecentDocument(p)
	}
}

// openRecentDocument opens path with the kind of its extension, or the
// first kind.
func openRecentDocument(path string) {
	kind := defaultDocKind()
	for _, k := range docKinds() {
		if k.handles(path) {
			kind = k
			break
		}
	}
	if kind == nil {
		return
	}
	if _, err := kind.open(path); err != nil {
		showDocumentError(nil, "The document “"+filepath.Base(path)+"” could not be opened.", err)
	}
}

// refreshDocumentMenus enables the document roles of the menus for the
// focused window's document. Main thread only.
func refreshDocumentMenus() {
	if !App.IsReady() {
		return
	}
	b := documentFor(FocusedWindow())
	var s DocumentState
	if b != nil {
		s = b.State()
	}
	enable := func(it *MenuItem) {
		var on bool
		switch it.Role {
		case RoleSave, RoleSaveAs, RoleDuplicate, RoleRename, RoleMoveTo:
			on = b != nil
		case RoleRevert:
			on = b != nil && s.Edited && s.Path != ""
		default:
			return
		}
		if it.IsEnabled() != on {
			it.SetEnabled(on)
		}
	}
	menus := []*Menu{App.Menu()}
	for _, w := range Windows() {
		menus = append(menus, w.menu)
	}
	for _, m := range menus {
		walkMenu(m, enable)
	}
}

// walkMenu calls fn with every item of m and its submenus.
func walkMenu(m *Menu, fn func(*MenuItem)) {
	if m == nil {
		return
	}
	for _, it := range m.Items() {
		fn(it)
		menuMu.Lock()
		sub := it.sub
		menuMu.Unlock()
		walkMenu(sub, fn)
	}
}

// The recent documents: those the app opened and saved, newest first,
// which Open Recent lists, kept in recent-documents.json in PathUserData
// (macOS keeps its own for the Dock, Windows the jump list's, Linux the
// recently used files: AddRecentDocument records them there too).
var recentDocs struct {
	sync.Mutex
	loaded bool
	paths  []string
}

const maxRecentDocuments = 10

func recentFile() (string, error) {
	dir, err := App.Path(PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "recent-documents.json"), nil
}

// loadRecentLocked reads the list once. recentDocs is locked.
func loadRecentLocked() {
	if recentDocs.loaded {
		return
	}
	recentDocs.loaded = true
	if f, err := recentFile(); err == nil {
		if data, err := os.ReadFile(f); err == nil {
			_ = json.Unmarshal(data, &recentDocs.paths)
		}
	}
}

// RecentDocuments returns the documents the app opened lately, newest
// first, which the Open Recent menu (RoleOpenRecent) lists.
func (a *Application) RecentDocuments() []string {
	recentDocs.Lock()
	defer recentDocs.Unlock()
	loadRecentLocked()
	return slices.Clone(recentDocs.paths)
}

// addRecent puts path first in the recent documents.
func addRecent(path string) {
	recentDocs.Lock()
	loadRecentLocked()
	paths := slices.DeleteFunc(slices.Clone(recentDocs.paths), func(p string) bool { return p == path })
	paths = slices.Insert(paths, 0, path)
	if len(paths) > maxRecentDocuments {
		paths = paths[:maxRecentDocuments]
	}
	recentDocs.paths = paths
	recentDocs.Unlock()
	saveRecent(paths)
	if App.IsReady() {
		if err := App.AddRecentDocument(path); err != nil {
			log.Printf("mygo: documents: %v", err)
		}
	}
}

// clearRecent empties the recent documents.
func clearRecent() {
	recentDocs.Lock()
	recentDocs.loaded = true
	recentDocs.paths = nil
	recentDocs.Unlock()
	saveRecent(nil)
	_ = App.ClearRecentDocuments()
}

func saveRecent(paths []string) {
	if f, err := recentFile(); err == nil {
		if err := writeAppFile(f, func(w io.Writer) error { return json.MarshalWrite(w, paths) }); err != nil {
			log.Printf("mygo: documents: %v", err)
		}
	}
	postMain(updateRecentMenus)
}

// recentMenuItems are the items of the Open Recent submenu.
func recentMenuItems() []*MenuItem {
	var items []*MenuItem
	for _, p := range App.RecentDocuments() {
		items = append(items, &MenuItem{Label: filepath.Base(p), ToolTip: p, Click: func(*MenuItem, *Window) {
			go openRecentDocument(p)
		}})
	}
	if len(items) > 0 {
		items = append(items, Separator())
	}
	return append(items, &MenuItem{Label: "Clear Menu", Disabled: len(items) == 0, Click: func(*MenuItem, *Window) {
		go clearRecent()
	}})
}

// updateRecentMenus gives the Open Recent submenus the recent documents.
// Main thread only.
func updateRecentMenus() {
	if !App.IsReady() {
		return
	}
	changed := false
	menus := []*Menu{App.Menu()}
	for _, w := range Windows() {
		menus = append(menus, w.menu)
	}
	for _, m := range menus {
		walkMenu(m, func(it *MenuItem) {
			if it.Role != RoleOpenRecent {
				return
			}
			items := recentMenuItems()
			menuMu.Lock()
			sub := &Menu{}
			for _, child := range items {
				sub.items = append(sub.items, prepareItem(child, sub))
			}
			it.Submenu, it.sub = items, sub
			menuMu.Unlock()
			changed = true
		})
	}
	if changed {
		refreshMenus()
	}
}

// The events of a document's pages: its state, and its content read
// again. Their names are MyGo's own.
const (
	documentStateEvent  = "mygo:document-state"
	documentReloadEvent = "mygo:document-reload"
)

// emitDocumentState tells w's page the state of its document. Paths are
// none of untrusted pages' business. Main thread only.
func emitDocumentState(w *Window, s DocumentState) {
	if w.trusted {
		if msg, err := encodeEvent(documentStateEvent, s); err == nil {
			w.enqueue(msg, true)
		}
	}
}

// emitDocumentReload tells w's page that its document's content was read
// again. Main thread only.
func emitDocumentReload(w *Window) {
	if w.trusted {
		if msg, err := encodeEvent(documentReloadEvent, struct{}{}); err == nil {
			w.enqueue(msg, true)
		}
	}
}
