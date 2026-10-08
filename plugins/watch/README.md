# @mygo-plugins/watch

Filesystem invalidation streams for MyGo pages. Configure Go access first:

```go
import "github.com/egoist/mygo/plugins/watch"

mygo.Use(watch.New(watch.Options{
    Roots: map[string]watch.Root{
        "project": {Path: projectDir, Recursive: true},
    },
}))
```

Roots must be absolute existing directories without symlink/reparse components,
controlled by the app. The default `watch.Plugin` denies all watches. This is
not a sandbox against hostile local processes moving/replacing directories.

```sh
bun add @mygo-plugins/watch
```

```ts
import { watch } from "@mygo-plugins/watch";
const watcher = await watch("project", { recursive: true });
try {
  for await (const event of watcher) console.log(event.op, event.path);
} finally {
  await watcher.close();
}
```

Setup resolves before events are consumed. Paths are root-relative; operations
are create/write/remove/rename. Events are coalesced hints, not a journal.
`closed` reports Go cleanup and terminal failures; optional `signal` cancels.
Overflow requires re-reading state and restarting. One iterator per watcher.
Go/native UI callers use `mygo.WatchFiles` without registering this plugin.

See the [full guide](https://github.com/egoist/mygo/blob/main/docs/plugins/watch.md)
for limits, error codes, lifecycle and platform differences.
