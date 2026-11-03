# Hot reload

`mygo dev -hot` changes the code of the running app as you save, usually in about
a tenth of a second. The process stays, and everything it holds stays with it:
its windows and where they are, the page in a web view and what its JavaScript
holds, open files and connections, and every value in memory.

```
  ✓ Reloaded (*counter).view (main.go changed)  67ms
```

It is opt-in: plain `mygo dev` restarts the app on every change. It needs no
change to your code. Native UI shows the
change in its next frame, since a view runs again for every frame it builds. A
web page keeps running, and its next call into Go runs the new code. Frontend
changes are still the dev server's, as Vite's hot module replacement.

A restart is what happens when a change cannot be applied in place, and `mygo
dev -hot` says why. It [keeps what it can](reload.md): where the windows are, the
page they show, scrolling and focus, and what the app hands over with
`App.OnReload`.

```
  • Restarting: the layout of main.counter changed
```

## What changes in place

Any function or method whose code changed, including calling new functions,
using new types, adding functions and methods that are not exported, using more
of a package the app already imports, and generics. Code that fails to compile
changes nothing: the compiler's message is shown and the app keeps running.

A package-level variable keeps the value it has, unless its initial value in the
source changed, which it then takes.

## What restarts

| Change | Why |
|---|---|
| The fields of a struct, or the layout of another type | Values of it are alive, in the old layout |
| The exported methods of a type | `mygo.Bind` and interfaces see them through reflection |
| `main`, and the closures in it | It ran already, and will not run again |
| `init` functions, and variables initialized by calling something | They ran as the app started, and running them again would reset its state |
| A new import of a package the app does not have yet | Its initialization needs to run |
| A new `.go` file, assembly, cgo | The build itself changes |
| The configuration, the icon, the resources, the catalogs, an embedded file | They are not code, or not only |

## Code that ran already

In-place changes apply from the next call of a function. What already ran does
not run again: a menu built once at startup keeps the items it has, and a
goroutine looping in a function stays in that function's old code, though what
it calls is new. So write what you are working on as a function that is called
again, as a view is:

```go
func (s *state) view(c *ui.Context) { ... } // changes in the next frame

func main() {
	s := &state{}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Content: ui.View(s.view)}) // main restarts
	})
	...
}
```

A view written as a function literal in `main` restarts the app when edited.

A closure that exists keeps its code, because what it captured is laid out as
that code knows. A callback registered once keeps running as it was until what
registers it runs again, though the functions it calls are new. Those made in a
view are new in the next frame. To run everything again from the start, type `r`
and Enter.

## When it runs

With `mygo dev -hot`, for the app's own module and the modules it replaces with a
directory, on macOS, Linux and Windows, on arm64 and amd64, with Go 1.27. With
another Go, or with `CGO_ENABLED=1`, it says so, and every change restarts the
app. It does not work with `-web`, which reloads the page.

With `-hot`, development builds of the app's own packages have inlining off, for
a function's callers not to hold copies of it. `mygo build` builds apps as before, and none
of this is in them.
