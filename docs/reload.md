# Reloading

`mygo dev` watches the app's Go code and restarts it as you edit. It
cannot keep the process: no platform lets a running Go program replace its
own functions, and MyGo builds without cgo, so an app is not a set of
plugins it could load again. A reload starts the app again instead — and
hands what should not be lost from the build that quits to the build that
starts.

Three things survive a reload, each the business of whoever holds it:

- MyGo keeps the [window](windows.md): where it was, its size, whether it
  was maximized, full screen or hidden, and whether it had the focus. A
  window takes the state of the window its `StateKey` names, so a build
  that creates its windows in another order still puts each state where it
  belongs; a window without one takes the next window that has none
  either, and two that share a key are paired in creation order. A window
  the reload has no state for starts fresh. The window is not shown until
  it has drawn the state it restored, so a reload does not flash an empty
  window.
- Package [ui](ui/README.md) keeps what its elements held: where a scroll
  container was scrolled, which element had the keyboard focus, and where
  the caret and the selection of the input the user was in were. That is
  [below](#what-package-ui-keeps).
- The app keeps its own state, its inputs and everything else, with
  `App.OnReload`: [below](#what-the-app-keeps).

Nothing is written to disk: the state lives in `mygo dev`'s memory for the
moment between the two builds, and the channel it travels on belongs to
that one session, so two `mygo dev` runs on the same project never mix
their apps. An app started by hand, a `mygo dev` started again, or a build
that fails to compile and leaves the old one running — none of them is
handed a state it did not just make.

## What package ui keeps

Elements are named by their place in the view and by the keys you gave them
with `Context.Key`, which the next build builds the same way. An element
that kept its key and its place gets back what it held:

```go
ui.Scroll(c.Key("list")).Grow(1).Children(view)
```

A [key](ui/views.md#keys) is what keeps an element the same element when
its neighbors change: with `Key("list")` the scroll position survives a
reload that adds a header above the list, and without it the element moved,
is another element now, and starts at the top. The keyboard focus and the
caret and the selection of the input the user was in are kept as well, so a
draft is still where it was after a reload that changed nothing about the
view. A password field keeps nothing.

The text itself is not part of this: `ui.TextInput(c, &s.draft)` reaches the
app as it is typed, so the app's own state is what an input shows, and
`Restore` brings it back before the first frame. Package ui adds where the
caret was in it, clamped to the text the app restored.

What a view keeps for itself with [`ui.Local`](ui/views.md#state-of-an-element)
is not handed over: it is the view's own scratch state, which may hold
anything and not just values that encode to JSON. An app that keeps, say,
which rows are open in a `ui.Local` map keeps it in its own state instead
and passes it into the view, as it does with everything else it holds.

## What the app keeps

Everything else is the app's to hand over, with `App.OnReload`, registered
before `App.Run`:

```go
type note struct {
	draft string
	lines []string
}

func main() {
	n := &note{}
	mygo.App.OnReload(mygo.ReloadHandlers{
		Save: func(r *mygo.ReloadState) error {
			if err := r.Set("draft", n.draft); err != nil {
				return err
			}
			return r.Set("lines", n.lines)
		},
		Restore: func(r *mygo.ReloadState) error {
			if _, err := r.Get("draft", &n.draft); err != nil {
				return err
			}
			_, err := r.Get("lines", &n.lines)
			return err
		},
	})
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "Reload",
			Content: ui.View(n.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
```

`Save` runs on the main thread in the build that is about to quit, where
the app's state is, and `Set` stores one value per key, which must encode
to JSON; a value that does not is reported in the dev output and the reload
goes on without it. What `Save` sets is the whole state the app hands over:
a key the code no longer sets is gone, so state of a build that changed
does not live on. `Restore` runs on the main thread in the build that
starts, before the `WhenReady` callbacks that create its windows, so an app
whose views are built from its state draws the restored state in its first
frame. Either handler may be nil.

`Get` reports whether the build before this one set the key, and fails with
what it could not read: a key that was never set is not an error, a value of
a shape this build does not know is. `Keys` lists the keys the build before
this one handed over, so an app can tell whether it is starting fresh.
Whatever the handlers do is MyGo's to keep:
`mygo.App.OnReload(mygo.ReloadHandlers{})` keeps only the windows and the
elements.

[`examples/reload-state`](https://github.com/egoist/mygo/tree/main/examples/reload-state)
is this in an app: a note and a counter that survive the reload.

## What a reload cannot keep

- **A web page.** The state of a page is in the web content process, which
  quits with the app. What lives in the app's profile is there again:
  cookies, `localStorage`, IndexedDB and caches
  ([browsing data](app.md#browsing-data)). A page that wants to survive a
  reload puts what matters in storage.
- **Anything that is not a value.** `Save` hands over JSON, not handles: a
  file being written, a socket, a subprocess, a media position or a
  background job has to be reopened or restarted by the new build. Leave a
  flag in the state to know what to do.
- **Browsers.** `mygo dev -web` reloads the page, which is the browser's
  own hot reload: there is no process of ours to hand a state to.

## Turning it off

`mygo dev -no-reload-state` starts every build fresh, as a reload before
this one did: the app is handed nothing, the handlers do not run, and what
package ui keeps is dropped. It is a development flag, like every flag of
`mygo dev`; `mygo build` marks its builds production, and the app it ships
never looks for a dev channel at all: like the inspector of native UI, the
handover is left out of its binary.
