# Watch

Watch existing directories from Go/native UI with `mygo.WatchFiles`, or expose
app-approved root aliases to pages with `@mygo-plugins/watch`. The page adapter
uses the same core watcher; Go callers do not need to register a plugin.

## Set up page access

```go
import "github.com/egoist/mygo/plugins/watch"

mygo.Use(watch.New(watch.Options{
    Roots: map[string]watch.Root{
        "project": {Path: projectDir, Recursive: true},
    },
}))
```

`projectDir` must be an absolute existing directory. `mygo.Use` resolves configured
symlinks (including macOS `/tmp` and `/var` aliases) once, then validates and pins
the canonical directory. Events stay relative to that canonical root; descendant
symlinks/reparse points are not followed. Configuration is copied and validated
without launching a GUI.
The default `watch.Plugin` denies all access. `Allow(ctx, alias)` may further
restrict configured aliases, for example using `mygo.CallerWindow(ctx)`; it
cannot grant access to another location. Recursive requests require permission.

```sh
bun add @mygo-plugins/watch
```

```ts
import { watch } from "@mygo-plugins/watch";
const watcher = await watch("project", { recursive: true, debounceMs: 75 });
try {
  for await (const event of watcher) console.log(event.op, event.path);
} finally {
  await watcher.close();
}
```

The promise resolves after setup, not after the watch ends. One iterator lazily
consumes events; breaking iteration closes the watch. `close()` is idempotent
and waits for Go cleanup. `closed` also settles after cleanup, rejecting on
failure. An optional `signal` aborts with its reason. Page navigation/window
closure cancels the channel. Each page generation has an independent quota:
8 watches by default (`MaxWatchesPerPage`), with a fixed service ceiling of 128.

## Go and native UI

Call after the application starts; use a goroutine for long waits:

```go
w, err := mygo.WatchFiles(ctx, directory, mygo.FileWatchOptions{Recursive: true})
if err != nil { return err }
defer w.Close()
for {
    event, err := w.Next(ctx)
    if err != nil { return err }
    invalidate(event.Path)
}
```

`Next` cancellation cancels only that wait; the constructor context cancels the
watch. Main-thread setup/Next pump native events. `Close` requests cleanup
without blocking; `Done` closes when cleanup finishes. Do not directly wait on
`Done` from the UI thread. Application shutdown closes watches before `OnQuit`.

## Semantics and recovery

Events are invalidation hints, **not a filesystem journal**. Paths are
slash-relative; `.` denotes the root. No initial listing is emitted. Operations
are `create`, `write` (including metadata), `remove`, and `rename` (`oldPath` is
the source). Rename pairing is best effort using unique identity evidence;
ambiguous moves use remove/create. Replacing an entry produces remove/create.
Directory renames do not synthesize a rename for every descendant.

Defaults: 50 ms trailing debounce, 500 ms maximum debounce delay, 1024 queued
events and 8192 snapshot entries. Go negative `Debounce` disables the quiet
period; page `debounceMs: 0` does the same. Page debounce accepts integers
0–60000 and raises maximum delay to match. Scanning/scheduling may add latency.
Repeated writes coalesce; create/remove within one window can disappear.
Symlinks/reparse descendants remain leaf entries and are never traversed.

Overflow stops the watch with `mygo.ErrWatchOverflow`; exceeding the entry cap
stops it with `mygo.ErrWatchLimit`. Re-read state and construct a new watcher.
Entries deleted during enumeration are skipped. Replacements during enrollment
are retried a bounded number of times, then skipped; an observed race retains a
write/create invalidation hint for the entry or its containing directory, even
during setup. Required-subtree permission errors, revoked watches and root
disappearance/identity changes remain terminal. There is no polling fallback or
automatic restart. `Err` retains the terminal failure, and explicit
close discards pending events. The page receives sanitized `WatchError` codes:
`denied`, `limit`, `overflow`, `unsupported`, `path-encoding`, or `io`.
Invalid UTF-8 filenames remain usable in Go but terminate page streams rather
than leaking ambiguous replacement-character paths. Transport errors remain
ordinary errors. Native paths are never included in page errors.

## Security and platform limits

**Only expose app-controlled roots protected against hostile local writers.**
Aliases prevent pages from selecting arbitrary paths; anchored enumeration and
identity-checked native registration mitigate races, but this is not a
kernel-enforced sandbox against another process replacing or moving directories.
No file contents are transmitted. No glob filters, file-only watches, nonexistent
roots, dynamic grants, persistent cursors or symlink following are supported.

- macOS 12+: kqueue vnode hints, a descriptor per regular file/directory. Process
  descriptor limits can fail setup below the configured entry cap.
- Linux: inotify, with recursive directory enrollment in Go. Kernel watch limits
  apply; queue overflow is terminal.
- Windows amd64/arm64: overlapped `ReadDirectoryChangesW`, a worker and 64 KiB
  buffer per directory. A private parent sentinel detects root identity changes
  without exposing siblings. Volume-root watches are unsupported.
- Unsupported OS/architectures return an unsupported error. Windows 386 keeps
  compiling through the existing unsupported adapter.

Local filesystems are the supported baseline. Network filesystems, mmap writes,
mount changes and external volumes have OS-dependent visibility. Broken remote
filesystem drivers can delay cleanup; pinned Windows buffers remain owned until
I/O completion. No content-complete-save or exactly-once guarantee is made.
