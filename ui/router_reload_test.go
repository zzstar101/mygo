//go:build !mygo_noinspector

package ui

import "testing"

// TestReloadNamesThePageOfTheRouteItRestores: a build that starts on the
// route the app was on shows the page the build that quit showed, so the
// focus and the caret come back to the element the user was in. The page
// is named after the path it shows, not after the number of pages the
// router happened to build before it.
func TestReloadNamesThePageOfTheRouteItRestores(t *testing.T) {
	value := ""
	view := func(r *Router) func(c *context) {
		return func(c *context) {
			r.coreView(c, func(rt *Route) {
				if rt.Match("/a") {
					coreText(c, "Page A")
					return
				}
				coreTextInput(c, &value).Label("Note").Width(200)
			})
		}
	}
	r1 := NewRouter("/a")
	r1.Transition = TransitionNone
	tt := reloadTester(view(r1), 400, 300, nil)
	r1.Push("/b")
	tt.Frame()
	if err := tt.Click("Note"); err != nil {
		t.Fatal(err)
	}
	tt.Type("hello")
	blob := tt.rt.reloadSave()
	if len(blob) == 0 {
		t.Fatal("a focused window saved nothing")
	}

	// The build that replaces it starts on the route the app restored.
	value = "hello"
	r2 := NewRouter("/b")
	r2.Transition = TransitionNone
	next := reloadTester(view(r2), 400, 300, blob)
	if !next.Focused("Note") {
		t.Errorf("after a reload on the route the app restored, the input the user was in has no focus")
	}
	next.Type("xy")
	if value != "helloxy" {
		t.Errorf("the input holds %q after a reload, want %q", value, "helloxy")
	}
}

// TestReloadNamesThePageOfALayoutAfterThePathItTakes: the pages of the
// entries under a layout are named after the part of the path the layout
// takes, so the layout's own elements keep their state through a reload
// that restored another page under the same layout.
func TestReloadNamesThePageOfALayoutAfterThePathItTakes(t *testing.T) {
	query := ""
	view := func(r *Router) func(c *context) {
		return func(c *context) {
			r.coreView(c, func(rt *Route) {
				if !rt.Match("/settings/{section...}") {
					coreText(c, "Not found")
					return
				}
				coreTextInput(c, &query).Label("Search").Width(200)
				rt.coreView(c, func(section *Route) {
					switch {
					case section.Match("/general"):
						coreText(c, "General")
					case section.Match("/fonts"):
						coreText(c, "Fonts")
					}
				})
			})
		}
	}
	r1 := NewRouter("/settings/general")
	r1.Transition = TransitionNone
	tt := reloadTester(view(r1), 400, 300, nil)
	r1.Push("/settings/fonts")
	tt.Frame()
	if err := tt.Click("Search"); err != nil {
		t.Fatal(err)
	}
	tt.Type("ab")
	blob := tt.rt.reloadSave()
	if len(blob) == 0 {
		t.Fatal("a focused window saved nothing")
	}

	// The build that replaces it starts on the page inside the layout.
	query = "ab"
	r2 := NewRouter("/settings/fonts")
	r2.Transition = TransitionNone
	next := reloadTester(view(r2), 400, 300, blob)
	if !next.Focused("Search") {
		t.Errorf("after a reload under a layout, the layout's input, which the user was in, has no focus")
	}
	next.Type("cd")
	if query != "abcd" {
		t.Errorf("the layout's input holds %q after a reload, want %q", query, "abcd")
	}
}

// TestReloadKeepsTheFocusOnThePage: the page itself keeps the focus of the
// user through a reload, not only the elements inside it, which is where
// the focus sits just after the app moved to the page.
func TestReloadKeepsTheFocusOnThePage(t *testing.T) {
	view := func(r *Router) func(c *context) {
		return func(c *context) {
			r.coreView(c, func(rt *Route) {
				switch {
				case rt.Match("/a"):
					rt.Title("Page A")
					coreText(c, "Page A")
				case rt.Match("/b"):
					rt.Title("Page B")
					coreText(c, "Page B")
				}
			})
		}
	}
	r1 := NewRouter("/a")
	r1.Transition = TransitionNone
	tt := reloadTester(view(r1), 400, 300, nil)
	r1.Push("/b")
	tt.Frame()
	if !tt.Focused("Page B") {
		t.Fatalf("the page the app moved to did not take the focus: %q", tt.Texts())
	}
	blob := tt.rt.reloadSave()
	if len(blob) == 0 {
		t.Fatal("a window whose focus is on a page saved nothing")
	}

	// The build that replaces it starts on the route the app restored.
	r2 := NewRouter("/b")
	r2.Transition = TransitionNone
	next := reloadTester(view(r2), 400, 300, blob)
	if !next.Focused("Page B") {
		t.Errorf("after a reload the page the user was on has no focus")
	}
}
