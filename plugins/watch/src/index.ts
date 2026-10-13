/**
 * The watch plugin of MyGo: changes to files in the directories the app
 * lets pages watch, by the names it gives them. The Go side must use the
 * plugin with those directories:
 *
 * ```go
 * mygo.Use(watch.New(watch.Options{ // github.com/egoist/mygo/plugins/watch
 * 	Roots: map[string]watch.Root{"project": {Path: dir, Recursive: true}},
 * }))
 * ```
 *
 * ```ts
 * import { watch } from "@mygo-plugins/watch";
 *
 * const watcher = await watch("project", { recursive: true });
 * for await (const event of watcher) console.log(event.op, event.path);
 * ```
 *
 * @module
 */
import { Channel, call, isCallError } from "mygo-runtime";

/** The kind of a {@link FileEvent}. */
export type FileOp = "create" | "write" | "remove" | "rename";

/**
 * A change in a watched directory. Events say what is different once
 * changes stop, not every step that made it so: treat them as hints to
 * read the files again.
 */
export interface FileEvent {
  op: FileOp;
  /** The path relative to the directory, separated by slashes; "." is the directory. */
  path: string;
  /** Where a rename moved from. */
  oldPath?: string;
  isDir: boolean;
}

/** Options of {@link watch}. */
export interface WatchOptions {
  /** Watch the directories in the directory too, if the app allows it. */
  recursive?: boolean;
  /** How long changes must stop before they are reported: 50 ms by default, 0 reports them right away. */
  debounceMs?: number;
  /** Stops the watch when aborted. */
  signal?: AbortSignal;
}

/** Why a watch failed or stopped; see {@link WatchError}. */
export type WatchErrorCode = "denied" | "limit" | "overflow" | "unsupported" | "path-encoding" | "io";

/** A watch that failed to start or stopped, with the reason as its code. */
export class WatchError extends Error {
  constructor(
    readonly code: WatchErrorCode,
    message: string,
  ) {
    super(message);
    this.name = "WatchError";
  }
}

/** A running watch: iterate it for the changes. */
export interface Watcher extends AsyncIterable<FileEvent> {
  /** Settles once Go has stopped the watch, rejecting with the error that stopped it. */
  readonly closed: Promise<void>;
  /** Stops the watch; it resolves once Go has. */
  close(): Promise<void>;
}

/** What Go streams. */
interface Part {
  type: "ready" | "events" | "error";
  events?: FileEvent[];
  code?: string;
  message?: string;
}

const codes: readonly string[] = ["denied", "limit", "overflow", "unsupported", "path-encoding", "io"];

/** Turns the errors of Go's "watch:<code>:<message>" into WatchErrors. */
function failure(error: unknown): unknown {
  if (isCallError(error)) {
    const match = /^watch:([^:]+):(.*)$/s.exec(error.message);
    if (match && codes.includes(match[1]!)) return new WatchError(match[1] as WatchErrorCode, match[2]!);
  }
  return error;
}

/**
 * Watches the directory the app named `root`, and resolves once everything
 * is watched. What is already there is not reported.
 */
export async function watch(root: string, options: WatchOptions = {}): Promise<Watcher> {
  const debounceMs = options.debounceMs ?? 50;
  if (!Number.isInteger(debounceMs) || debounceMs < 0 || debounceMs > 60000) {
    throw new TypeError("debounceMs must be an integer between 0 and 60000");
  }
  if (options.signal?.aborted) throw options.signal.reason;
  const parts = new Channel<Part>();
  const stream = parts[Symbol.asyncIterator]();
  // The first error wins: the signal's reason, Go's error part, or the call's.
  let terminal: { error: unknown } | undefined;
  let closing = false;
  let finished = false;
  let iterating = false;
  const record = (error: unknown) => {
    terminal ??= { error };
  };
  const abort = () => {
    if (finished || closing) return;
    record(options.signal!.reason);
    closing = true;
    parts.close();
  };
  options.signal?.addEventListener("abort", abort, { once: true });
  const closed = call<void>("plugin:watch.Watch", { root, recursive: options.recursive ?? false, debounceMs }, parts)
    .then(
      () => {
        if (terminal) throw terminal.error;
      },
      (err: unknown) => {
        record(failure(err));
        throw terminal!.error;
      },
    )
    .finally(() => {
      finished = true;
      options.signal?.removeEventListener("abort", abort);
    });
  void closed.catch(() => {});
  // Records an error part, and reports whether part is not one.
  const check = (part: Part) => {
    if (part.type !== "error") return true;
    record(
      codes.includes(part.code ?? "")
        ? new WatchError(part.code as WatchErrorCode, part.message ?? "filesystem watch stopped")
        : new Error("Invalid watch error response"),
    );
    return false;
  };
  const close = async () => {
    if (!closing && !finished) {
      closing = true;
      parts.close();
    }
    await closed.catch(() => {});
  };
  try {
    const first = await stream.next();
    if (first.done) {
      await closed;
      throw new Error("Watch ended before ready");
    }
    if (!check(first.value) || first.value.type !== "ready") {
      parts.close();
      await closed;
      throw new Error("Invalid watch ready response");
    }
    if (terminal) {
      await closed;
      throw terminal.error;
    }
  } catch (err) {
    parts.close();
    options.signal?.removeEventListener("abort", abort);
    throw err;
  }
  return {
    closed,
    close,
    [Symbol.asyncIterator]() {
      if (iterating) throw new Error("A watch supports one iterator");
      iterating = true;
      return (async function* () {
        try {
          while (!closing) {
            const next = await stream.next();
            if (next.done) break;
            if (!check(next.value)) {
              parts.close();
              break;
            }
            for (const event of next.value.events ?? []) {
              if (closing) break;
              yield event;
            }
          }
          await closed;
        } finally {
          await close();
          iterating = false;
        }
      })();
    },
  };
}
