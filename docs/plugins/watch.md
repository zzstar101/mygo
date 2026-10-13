# Watch

The watch plugin lets pages follow changes to files in the directories the
app lets them watch, such as a project open in an editor. Go code and
native UI don't need it: they call `mygo.WatchFiles` directly (see
[Watching files](../native.md#watching-files)), which the plugin uses too.

## Set up

Use the plugin in Go with the directories pages may watch, each under a
name:

```go
import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/watch"
)

func main() {
	mygo.Use(watch.New(watch.Options{
		Roots: map[string]watch.Root{
			"project": {Path: projectDir, Recursive: true},
		},
	}))
	// ...
}
```

and add its package to the frontend:

```sh
bun add @mygo-plugins/watch
```

Pages name a directory by its name in `Roots`, never by a path, so they
can't watch anything else. A name is lowercase letters, digits and dashes,
starting with a letter. A path must be absolute and a directory when
`mygo.Use` runs, which panics otherwise; it resolves the path's symlinks,
as macOS's `/tmp` is one. `watch.Plugin`, without roots, lets pages watch
nothing.

`Recursive` lets pages watch the directories in the directory too; without
it they can only watch the directory itself. `Allow`, when set, is asked
before each watch, with the call's context (`mygo.CallerWindow(ctx)` is the
window asking) and the name, and can refuse it:

```go
watch.New(watch.Options{
	Roots: roots,
	Allow: func(ctx context.Context, root string) bool {
		return mygo.CallerWindow(ctx) == editorWindow
	},
})
```

Each page can watch 8 directories at a time (`MaxWatchesPerPage`), and all
pages 128.

## Watching

`watch` starts a watch and resolves once everything is watched. Iterate it
for the changes:

```ts
import { watch } from "@mygo-plugins/watch";

const watcher = await watch("project", { recursive: true });
for await (const event of watcher) {
  console.log(event.op, event.path, event.oldPath);
}
```

An event has `op`, one of `create`, `write`, `remove` and `rename`, `path`,
relative to the directory and separated by slashes (`.` is the directory
itself), `oldPath`, where a rename moved from, and `isDir`. What is already
there when the watch starts is not reported.

Changes wait for 50 ms of quiet before they are reported, and never more
than 500 ms while more keep coming; `debounceMs` sets the quiet, and `0`
reports them right away. A watch compares what it sees with what it saw,
so events say what is different, not every step that made it so: a file
written many times in a row is one write, and one created and removed in
between is nothing. Treat them as hints to read the files again.

`watcher.close()`, `break` out of the loop, or an `AbortSignal` passed as
`signal` stop the watch, and so does the page navigating or closing.
`watcher.closed` settles once Go has stopped it, and rejects with the
error that stopped it. Errors are `WatchError`s, whose `code` is:

- `denied`: the name is not one of the roots, `Allow` refused, or the page
  asked for a recursive watch of a root without `Recursive`;
- `limit`: the page or the app watch too many directories, or the directory
  has more than 8192 files and directories;
- `overflow`: more changes waited than the page read, and some were lost;
- `unsupported`: the platform can't watch files;
- `path-encoding`: a file's name is not valid UTF-8, which pages can't be
  given;
- `io`: anything else, such as the directory being removed.

After `overflow`, read the files again and start a new watch.

## Platforms

Watches follow what is in the directory, not where symlinks and Windows
junctions in it point. Only expose directories the app controls: names
keep pages from choosing paths, but another program can still move or
replace what is in a directory.

- macOS watches with kqueue, which takes a file descriptor for every file
  and directory watched.
- Linux watches each directory with inotify, whose limits are those of the
  system (`fs.inotify.max_user_watches`, and `max_user_instances`, as each
  watch takes an instance).
- Windows watches a recursive directory with one `ReadDirectoryChangesW`,
  so the directories in it can be renamed, and also watches its parent to
  know when it is renamed. While a directory is watched, Windows doesn't let
  its parent and the directories above be renamed, as for every program
  that watches files. It can't watch the root of a drive.
