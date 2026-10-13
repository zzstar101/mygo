# @mygo-plugins/watch

Changes to files for [MyGo](https://github.com/egoist/mygo) apps' pages, in
the directories the app lets them watch, such as a project open in an
editor.

Use the plugin in Go with the directories pages may watch, each under a
name:

```go
import "github.com/egoist/mygo/plugins/watch"

mygo.Use(watch.New(watch.Options{
	Roots: map[string]watch.Root{
		"project": {Path: projectDir, Recursive: true},
	},
}))
```

and watch one by its name in the frontend:

```ts
import { watch } from "@mygo-plugins/watch";

const watcher = await watch("project", { recursive: true });
for await (const event of watcher) {
  console.log(event.op, event.path); // "write", "src/main.ts"
}
```

Events are `create`, `write`, `remove` and `rename` (with `oldPath`), with
paths relative to the directory. They say what is different after changes
stop for 50 ms (`debounceMs`), not every step: treat them as hints to read
the files again. `watcher.close()`, breaking out of the loop, an
`AbortSignal` (`signal`) or the page navigating stop the watch.

[The documentation](https://github.com/egoist/mygo/blob/main/docs/plugins/watch.md)
covers the options of the Go side, errors and how each platform watches.
