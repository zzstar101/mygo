package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// What package ui keeps across a reload of `mygo dev`: where its elements
// were scrolled, which one had the focus, and where the caret and the
// selection of the input the user was in were. What an input holds is the
// app's own state, which App.OnReload brings back: the text of an input is
// never taken from here.
//
// Elements are named by their place in the view, which the next build
// builds the same way: the position of the element in the tree, and the
// keys it was given with Key. An element that moved, or whose neighbors
// changed, is not found again and its state is dropped: guessing would be
// worse than starting fresh.

// reloadEntry is what one element keeps.
type reloadEntry struct {
	// ID names the element: see reloadKey.
	ID uint64 `json:"id"`
	// ScrollX and ScrollY are the offset of a scroll container.
	ScrollX float64 `json:"x,omitempty"`
	ScrollY float64 `json:"y,omitempty"`
	// Focused tells that the element had the keyboard focus.
	Focused bool `json:"f,omitempty"`
	// Sel tells that the input had a caret, and where: what an app holds
	// itself comes back with its own state, and the caret is placed on it.
	// A password field keeps nothing.
	Sel    bool `json:"s,omitempty"`
	Caret  int  `json:"c,omitempty"`
	Anchor int  `json:"a,omitempty"`
}

// reloadBlob is what a window hands from one build of `mygo dev` to the
// next.
type reloadBlob struct {
	Entries []reloadEntry `json:"entries,omitempty"`
}

// reloadSave returns the state of the window's elements, for the build
// that starts next. Main thread only, with the state of the last frame.
func (h *windowHost) reloadSave() []byte {
	if h.rt == nil {
		return nil
	}
	return h.rt.reloadSave()
}

// reloadRestore takes the state of the build this one replaces: the
// elements it names get it back as they are created, in the first frame.
// Main thread only, before that frame.
func (h *windowHost) reloadRestore(data []byte) {
	if h.rt == nil {
		return
	}
	h.rt.reloadRestore(data)
}

// reloadSave collects what the elements of the last frame kept.
func (rt *engine) reloadSave() []byte {
	entries := make([]reloadEntry, 0, 8)
	for _, st := range rt.states {
		if st.reloadID == 0 {
			continue
		}
		e := reloadEntry{ID: st.reloadID, ScrollX: st.scrollX, ScrollY: st.scrollY}
		if rt.focused == st.id {
			e.Focused = true
			// The caret and the selection of the input the user was in.
			// A password field keeps nothing of what it holds.
			if ed := st.editor; ed != nil && !ed.password {
				e.Sel = true
				e.Caret, e.Anchor = ed.caret, ed.anchor
			}
		}
		if e.ScrollX == 0 && e.ScrollY == 0 && !e.Focused {
			continue
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	data, err := json.Marshal(reloadBlob{Entries: entries})
	if err != nil {
		return nil
	}
	return data
}

// reloadRestore takes the state of the build this one replaces, to apply
// to the elements as it creates them.
func (rt *engine) reloadRestore(data []byte) {
	var blob reloadBlob
	if json.Unmarshal(data, &blob) != nil {
		return
	}
	rt.reloadApply = make(map[uint64]reloadEntry, len(blob.Entries))
	for _, e := range blob.Entries {
		rt.reloadApply[e.ID] = e
	}
}

// reloadBorn names an element that appears in this frame, so the next
// build of the same view finds it again, and applies what the build before
// this one kept of it.
func (rt *engine) reloadBorn(e *node) {
	if !rt.reload || e.st.born != rt.frame {
		return
	}
	e.st.reloadID = e.reloadID
	if rt.reloadApply != nil {
		rt.reloadAt(e)
	}
}

// reloadAt applies what the build before this one kept of e, which is
// created in this frame.
func (rt *engine) reloadAt(e *node) {
	ent, ok := rt.reloadApply[e.reloadID]
	if !ok {
		return
	}
	// Once: the state of an element outlives its frame.
	delete(rt.reloadApply, e.reloadID)
	st := e.st
	if ent.ScrollX != 0 || ent.ScrollY != 0 {
		st.reloadScroll = true
		st.reloadScrollX, st.reloadScrollY = ent.ScrollX, ent.ScrollY
	}
	if ent.Sel {
		st.reloadSel = true
		st.reloadCaret, st.reloadAnchor = ent.Caret, ent.Anchor
	}
	if ent.Focused {
		// As AutoFocus: the element takes the focus as it appears.
		e.Focus()
	}
}

// reloadKey hashes a key the same way in every build, unlike keyedID,
// whose seed is random in each process. The type of the key is part of
// what is hashed: the string "1" and the number 1 are different keys, and
// would otherwise name the same element.
func reloadKey(k any) uint64 {
	var s string
	switch k := k.(type) {
	case string:
		s = "s:" + k
	case int:
		s = "i:" + strconv.Itoa(k)
	case int64:
		s = "i64:" + strconv.FormatInt(k, 10)
	case uint64:
		s = "u64:" + strconv.FormatUint(k, 10)
	default:
		s = fmt.Sprintf("%T:%v", k, k)
	}
	const offset64, prime64 = 14695981039346656037, 1099511628211
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h ^ 0xA5A5A5A5A5A5A5A5
}
