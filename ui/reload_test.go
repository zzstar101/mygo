package ui

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

// reloadView is the view the reload tests drive: a text input the user
// typed in, and a list that scrolls. extra inserts an element between them,
// as an edit that moves the list down does, and keyed gives the list a key,
// which names it whatever moves around it.
func reloadView(value *string, rows int, extra, keyed bool) func(c *context) {
	return func(c *context) {
		coreColumn(c).Children(func() {
			coreTextInput(c, value).Label("Name").Width(200)
			if extra {
				coreText(c, "Header").Height(20)
			}
			list := coreScroll(c)
			if keyed {
				list.Key("list")
			}
			list.Size(300, 200).Children(func() {
				for i := range rows {
					coreText(c, fmt.Sprintf("Row %02d", i)).Height(20)
				}
			})
		})
	}
}

// reloadTester starts a window as `mygo dev` starts the build that
// replaces another: elements are named (reload), and blob, when there is
// one, is the state of the build before it.
func reloadTester(view func(c *context), width, height int, blob []byte) *Tester {
	h := &headless{w: float32(width), h: float32(height), scale: 1}
	t := &Tester{rt: newRuntime(view, h), h: h}
	t.rt.collect = true
	t.rt.strict = true
	t.rt.handleChecks = true
	t.rt.reload = true
	if blob != nil {
		t.rt.reloadRestore(blob)
	}
	t.settle()
	return t
}

// reloadScrollY returns how far the view's scroll container is scrolled.
func reloadScrollY(t *Tester) float64 {
	for _, st := range t.rt.states {
		if st.scrollY != 0 {
			return st.scrollY
		}
	}
	return 0
}

func TestReloadKeepsScrollFocusAndCaret(t *testing.T) {
	value := ""
	tt := reloadTester(reloadView(&value, 40, false, false), 400, 300, nil)
	if err := tt.Click("Name"); err != nil {
		t.Fatal(err)
	}
	tt.Type("abc")
	tt.Scroll(150, 150, 0, 120)
	saved := reloadScrollY(tt)
	if saved == 0 {
		t.Fatal("the list did not scroll")
	}
	blob := tt.rt.reloadSave()
	if len(blob) == 0 {
		t.Fatal("a scrolled, focused window saved nothing")
	}

	// The build that replaces it starts with the app's state, which the
	// app restored before the first frame; the elements keep where they
	// were scrolled and where the caret was.
	value = "abc"
	next := reloadTester(reloadView(&value, 40, false, false), 400, 300, blob)
	if value != "abc" {
		t.Errorf("the reload wrote %q over the app's state", value)
	}
	if !next.Focused("Name") {
		t.Error("the focus did not come back")
	}
	if got := reloadScrollY(next); got != saved {
		t.Errorf("the list is scrolled to %v after a reload, want %v", got, saved)
	}
	if _, ok := next.Find("Row 06"); !ok {
		t.Errorf("the rows brought back into view are not drawn: %q", next.Texts())
	}
}

// TestReloadDoesNotOverwriteTheAppValue is the promise of App.OnReload
// being the app's own: an input's text is not handed over, and what the
// app restored is what the build shows, focused, with the caret in it.
func TestReloadDoesNotOverwriteTheAppValue(t *testing.T) {
	value := ""
	tt := reloadTester(reloadView(&value, 10, false, false), 400, 300, nil)
	if err := tt.Click("Name"); err != nil {
		t.Fatal(err)
	}
	tt.Type("typed in the old build")
	blob := tt.rt.reloadSave()
	if len(blob) == 0 {
		t.Fatal("the focused input saved nothing")
	}

	// The app restores a value of its own, which is not what was typed.
	value = "restored by the app"
	next := reloadTester(reloadView(&value, 10, false, false), 400, 300, blob)
	if value != "restored by the app" {
		t.Errorf("the reload wrote %q over the app's state", value)
	}
	if texts := next.Texts(); slices.Contains(texts, "typed in the old build") {
		t.Errorf("the input held on to what the old build had: %q", texts)
	}
	ed := reloadFocusedEditor(next)
	if ed == nil {
		t.Fatal("the input did not come back focused")
	}
	if got := ed.String(); got != "restored by the app" {
		t.Errorf("the input shows %q, want the app's own state", got)
	}
	if !next.Focused("Name") {
		t.Error("the focus did not come back")
	}
}

// TestReloadClampsTheCaretToTheRestoredText checks that the caret of an
// input lands in the text the app restored, whatever its length.
func TestReloadClampsTheCaretToTheRestoredText(t *testing.T) {
	value := "old text"
	tt := reloadTester(reloadView(&value, 10, false, false), 400, 300, nil)
	if err := tt.Click("Name"); err != nil {
		t.Fatal(err)
	}
	blob := reloadBlobWithCaret(t, tt.rt.reloadSave(), 100, 90)

	// The app restores a much shorter text.
	value = "short"
	next := reloadTester(reloadView(&value, 10, false, false), 400, 300, blob)
	ed := reloadFocusedEditor(next)
	if ed == nil {
		t.Fatal("the input did not come back focused")
	}
	if ed.caret != len("short") || ed.anchor != len("short") {
		t.Errorf("the caret is at %d/%d, want %d", ed.caret, ed.anchor, len("short"))
	}
	if value != "short" {
		t.Errorf("the reload wrote %q over the app's state", value)
	}
}

// TestReloadKeepsNothingOfAPassword checks that a password field hands
// nothing of what it holds over, not even where its caret was.
func TestReloadKeepsNothingOfAPassword(t *testing.T) {
	value := ""
	view := func(c *context) {
		coreColumn(c).Children(func() {
			coreTextInput(c, &value).Label("Password").Password()
		})
	}
	tt := reloadTester(view, 400, 300, nil)
	if err := tt.Click("Password"); err != nil {
		t.Fatal(err)
	}
	tt.Type("secret")
	blob := tt.rt.reloadSave()
	if len(blob) == 0 {
		t.Fatal("the focused password field saved nothing at all")
	}
	var b reloadBlob
	if err := json.Unmarshal(blob, &b); err != nil {
		t.Fatal(err)
	}
	for _, ent := range b.Entries {
		if ent.Sel || ent.Caret != 0 || ent.Anchor != 0 {
			t.Errorf("a password field handed over %+v", ent)
		}
	}
}

// TestReloadKeysDoNotCollideAcrossTypes checks that the same value as a
// string and as a number, and the several number types, name elements of
// their own: "1" and 1 are keys an app may use in the same view.
func TestReloadKeysDoNotCollideAcrossTypes(t *testing.T) {
	seen := map[uint64]any{}
	for _, k := range []any{"1", 1, int64(1), uint64(1), "float64:1", float64(1), true, "true"} {
		id := reloadKey(k)
		if other, ok := seen[id]; ok {
			t.Errorf("the keys %#v and %#v name the same element", other, k)
		}
		seen[id] = k
	}
}

// TestReloadKeepsKeyedElementsOfDifferentTypesApart is the same promise
// through a view: two scroll containers keyed "1" and 1 keep the scroll
// they had, each of its own.
func TestReloadKeepsKeyedElementsOfDifferentTypesApart(t *testing.T) {
	view := func(c *context) {
		coreColumn(c).Children(func() {
			coreScroll(c).Key("1").Size(200, 100).Children(func() {
				for i := range 20 {
					coreText(c, fmt.Sprintf("S %02d", i)).Height(20)
				}
			})
			coreScroll(c).Key(1).Size(200, 100).Children(func() {
				for i := range 20 {
					coreText(c, fmt.Sprintf("N %02d", i)).Height(20)
				}
			})
		})
	}
	tt := reloadTester(view, 400, 400, nil)
	tt.Scroll(100, 50, 0, 60)
	tt.Scroll(100, 200, 0, 40)
	saved := reloadScrollTotal(tt)
	if saved == 0 {
		t.Fatal("the containers did not scroll")
	}
	blob := tt.rt.reloadSave()
	next := reloadTester(view, 400, 400, blob)
	if got := reloadScrollTotal(next); got != saved {
		t.Errorf("the keyed containers are scrolled to %v after a reload, want %v", got, saved)
	}
}

// reloadBlobWithCaret is a blob of a focused input, with its caret moved
// where the new build's text may not reach.
func reloadBlobWithCaret(t *testing.T, blob []byte, caret, anchor int) []byte {
	t.Helper()
	var b reloadBlob
	if err := json.Unmarshal(blob, &b); err != nil {
		t.Fatal(err)
	}
	moved := false
	for i := range b.Entries {
		if b.Entries[i].Sel {
			b.Entries[i].Caret, b.Entries[i].Anchor = caret, anchor
			moved = true
		}
	}
	if !moved {
		t.Fatal("the blob holds no caret to move")
	}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// reloadFocusedEditor is the editor of the element that has the focus.
func reloadFocusedEditor(t *Tester) *editor {
	for _, st := range t.rt.states {
		if st.id == t.rt.focused && st.editor != nil {
			return st.editor
		}
	}
	return nil
}

// reloadScrollTotal is how far the view is scrolled in all, the sum of
// every scroll container.
func reloadScrollTotal(t *Tester) float64 {
	total := 0.0
	for _, st := range t.rt.states {
		total += st.scrollY
	}
	return total
}

// TestReloadDropsStateOfAMovedElement keeps the promise the docs make: an
// element whose place in the view changed is not found again, and starts
// fresh, where guessing would be worse.
func TestReloadDropsStateOfAMovedElement(t *testing.T) {
	value := ""
	tt := reloadTester(reloadView(&value, 40, false, false), 400, 300, nil)
	if err := tt.Click("Name"); err != nil {
		t.Fatal(err)
	}
	tt.Type("abc")
	tt.Scroll(150, 150, 0, 120)
	if reloadScrollY(tt) == 0 {
		t.Fatal("the list did not scroll")
	}
	blob := tt.rt.reloadSave()

	// A build whose view put an element before the list: the list moved.
	// The app restored what it holds itself, which the elements do not
	// touch.
	value = "abc"
	next := reloadTester(reloadView(&value, 40, true, false), 400, 300, blob)
	if got := reloadScrollY(next); got != 0 {
		t.Errorf("a list that moved kept the scroll %v", got)
	}
	// The input did not move, and keeps what it had.
	if value != "abc" || !next.Focused("Name") {
		t.Errorf("the unmoved input holds %q, focus %v", value, next.Focused("Name"))
	}
}

// TestReloadFollowsAKeyedElement is the other half: an element given a key
// is found again whatever moved around it, as Key promises.
func TestReloadFollowsAKeyedElement(t *testing.T) {
	value := ""
	tt := reloadTester(reloadView(&value, 40, false, true), 400, 300, nil)
	tt.Scroll(150, 150, 0, 120)
	saved := reloadScrollY(tt)
	if saved == 0 {
		t.Fatal("the list did not scroll")
	}
	blob := tt.rt.reloadSave()

	next := reloadTester(reloadView(&value, 40, true, true), 400, 300, blob)
	if got := reloadScrollY(next); got != saved {
		t.Errorf("a keyed list that moved is scrolled to %v, want %v", got, saved)
	}
}

// TestReloadNamesElementsTheSameWayInEveryBuild is what the handover rests
// on: the same view names its elements the same way in every process,
// unlike the IDs the engine uses, whose seed is random per process.
func TestReloadNamesElementsTheSameWayInEveryBuild(t *testing.T) {
	ids := func() []uint64 {
		value := ""
		tt := reloadTester(reloadView(&value, 3, false, false), 400, 300, nil)
		out := make([]uint64, 0, 8)
		for _, st := range tt.rt.states {
			if st.reloadID != 0 {
				out = append(out, st.reloadID)
			}
		}
		if len(out) == 0 {
			t.Fatal("the view named no element")
		}
		slices.Sort(out)
		return out
	}
	if a, b := ids(), ids(); !slices.Equal(a, b) {
		t.Errorf("the same view named its elements %v and %v", a, b)
	}
}
