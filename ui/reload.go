//go:build !mygo_noinspector

package ui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/internal/surface"
)

// reloadable tells that this build keeps the state of its elements across a
// reload of mygo dev. Production builds, which leave the developer tools out
// (reload_off.go), leave it all out too: not a byte, and not a field.
const reloadable = true

// nodeReload names an element across builds: same view, same name.
type nodeReload struct{ reloadID uint64 }

// stateReload is what an element keeps across builds: its name, and what the
// build before this one kept of it until the element is built.
type stateReload struct {
	reloadID   uint64
	reloadHeld *reloadEntry
}

// engineReload is what a window keeps across builds.
type engineReload struct {
	// reload is set when mygo dev launched the window with state handover:
	// elements then carry a name, and keep what a reload should give back.
	reload bool
	// reloadApply is what the build before this one kept, until the elements
	// it names are built.
	reloadApply map[uint64]reloadEntry
	// reloadRouters are the routers of the window, in the order they first
	// showed, and reloadHistories what the build before this one kept of its
	// own, until the router of the same place in that order shows.
	reloadRouters   []*Router
	reloadHistories []reloadHistory
}

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
	Entries []reloadEntry   `json:"entries,omitempty"`
	Routers []reloadHistory `json:"routers,omitempty"`
}

// reloadHistory is where a router was: its history, and the entry shown.
type reloadHistory struct {
	Entries []string `json:"entries"`
	At      int      `json:"at"`
}

// reloadRouter notes a router as it shows, and puts it where the router in its
// place was in the build before this one. Routers are told apart by the order
// they first show in, not by their place in the view: an edit that needs a
// restart often changes what is around the router. An app that navigated
// already, in Restore, has its own idea of where to be. Main thread only.
func (rt *engine) reloadRouter(r *Router) {
	if !rt.reload {
		return
	}
	for _, seen := range rt.reloadRouters {
		if seen == r {
			return
		}
	}
	at := len(rt.reloadRouters)
	rt.reloadRouters = append(rt.reloadRouters, r)
	if at >= len(rt.reloadHistories) || len(r.entries) > 1 {
		return
	}
	h := rt.reloadHistories[at]
	if h.At < 0 || h.At >= len(h.Entries) {
		return
	}
	r.entries = make([]*routeEntry, len(h.Entries))
	for i, loc := range h.Entries {
		r.entries[i] = &routeEntry{loc: loc}
	}
	r.at = h.At
}

func (rt *engine) saveRouters() []reloadHistory {
	var out []reloadHistory
	for _, r := range rt.reloadRouters {
		h := reloadHistory{At: r.at, Entries: make([]string, len(r.entries))}
		for i, e := range r.entries {
			h.Entries[i] = e.loc
		}
		out = append(out, h)
	}
	return out
}

// attachReload names the window's elements if mygo dev launched it with state
// handover, so that the next build finds them again.
func (h *windowHost) attachReload(conn *surface.Conn) {
	if conn.Reload {
		h.rt.reload = true
		conn.ReloadSave = h.reloadSave
		conn.ReloadRestore = h.reloadRestore
	}
}

// reloadNameRoot names the root of a tree: the window's, or an overlay's.
func (e *node) reloadNameRoot() { e.reloadID = e.id }

// reloadNamePlace names an element by its place under its parent.
func (rt *engine) reloadNamePlace(e, parent *node, place uint64) {
	if rt.reload {
		e.reloadID = mix(parent.reloadID, place)
	}
}

// reloadNameKey names an element by its key under its parent, if any.
func (rt *engine) reloadNameKey(e, parent *node, k any) {
	if !rt.reload {
		return
	}
	var id uint64
	if parent != nil {
		id = parent.reloadID
	}
	e.reloadID = mix(id, reloadKey(k))
}

// reloadTakeScroll scrolls where the build before this one was scrolled,
// which layout then clamps to what it scrolls now.
func (s *state) reloadTakeScroll(frame uint64) {
	if h := s.reloadHeld; h != nil && (h.ScrollX != 0 || h.ScrollY != 0) {
		s.beginMove(frame)
		s.scrollTo(max(0, h.ScrollX), max(0, h.ScrollY))
		h.ScrollX, h.ScrollY = 0, 0
	}
}

// reloadTakeCaret puts the caret and the selection of the input the user was
// in where they were. Its text is the app's own state, which Restore brought
// back before this frame, and is not touched here.
func (s *state) reloadTakeCaret(ed *editor) {
	if h := s.reloadHeld; h != nil && h.Sel {
		h.Sel = false
		ed.caret = min(h.Caret, ed.buf.n)
		ed.anchor = min(h.Anchor, ed.buf.n)
	}
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
	routers := rt.saveRouters()
	if len(entries) == 0 && len(routers) == 0 {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	data, err := json.Marshal(reloadBlob{Entries: entries, Routers: routers})
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
	rt.reloadHistories = blob.Routers
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
	if ent.ScrollX != 0 || ent.ScrollY != 0 || ent.Sel {
		e.st.reloadHeld = &ent
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

// namePage names the page after the parts of the path it shows, the ones
// of the page itself and of the layouts around it, so that a build that
// restores the route names the page as the build that quit did, and the
// elements inside keep their state through a reload of mygo dev. The
// number of a page in the history cannot name it: a build that starts on
// another route numbers its pages differently. parts is how many parts of
// the path the page shows, the whole path at first, then what a {name...}
// of a layout takes.
func (r *Route) namePage(parts int) {
	pg := r.page
	if pg == nil || r.router == nil || r.router.rt == nil || !r.router.rt.reload {
		// Only a window mygo dev launched with state handover names its
		// elements across builds: other apps do not parse the path of
		// every page in every frame for it.
		return
	}
	segments := segmentsOf(r.entry.loc)
	if parts < 0 || parts > len(segments) {
		parts = len(segments)
	}
	var parent uint64
	if pg.parent != nil {
		parent = pg.parent.reloadID
	}
	key := "page:" + strconv.Itoa(r.level) + ":" + strings.Join(segments[:parts], "/")
	pg.reloadID = mix(parent, reloadKey(key))
	if pg.st != nil {
		// The page was born under the number the router gave it: its own
		// state is kept under the name it shows, as its elements are.
		pg.st.reloadID = pg.reloadID
	}
	if rt := r.router.rt; rt.reloadApply != nil {
		rt.reloadAt(pg)
	}
}
