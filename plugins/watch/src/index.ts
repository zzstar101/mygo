import { Channel, call, isCallError } from "mygo-runtime";

export type FileOp = "create" | "write" | "remove" | "rename";
export interface FileEvent {
  op: FileOp;
  /** Slash-relative to the selected root; never an absolute host path. */
  path: string;
  oldPath?: string;
  isDir: boolean;
}
export interface WatchOptions {
  recursive?: boolean;
  debounceMs?: number;
  signal?: AbortSignal;
}
export type WatchErrorCode = "denied" | "limit" | "overflow" | "unsupported" | "path-encoding" | "io";
export class WatchError extends Error {
  constructor(readonly code: WatchErrorCode, message: string) {
    super(message);
    this.name = "WatchError";
  }
}
export interface Watcher extends AsyncIterable<FileEvent> {
  /** Settles after Go cleanup; rejects on failure or signal abort. */
  readonly closed: Promise<void>;
  /** Idempotent clean cancellation, completed after Go cleanup. */
  close(): Promise<void>;
}
interface Part {
  type: "ready" | "events" | "error";
  events?: FileEvent[];
  code?: string;
  message?: string;
}
const codes: readonly string[] = ["denied", "limit", "overflow", "unsupported", "path-encoding", "io"];
function failure(error: unknown): unknown {
  if (isCallError(error)) {
    const match = /^watch:([^:]+):(.*)$/s.exec(error.message);
    if (match && codes.includes(match[1]!)) return new WatchError(match[1] as WatchErrorCode, match[2]!);
  }
  return error;
}
/** Watches an app-configured root alias, resolving after native setup. */
export async function watch(root: string, options: WatchOptions = {}): Promise<Watcher> {
  const debounceMs = options.debounceMs ?? 50;
  if (!Number.isInteger(debounceMs) || debounceMs < 0 || debounceMs > 60000) {
    throw new TypeError("debounceMs must be an integer between 0 and 60000");
  }
  if (options.signal?.aborted) throw options.signal.reason;
  const parts = new Channel<Part>();
  const stream = parts[Symbol.asyncIterator]();
  let terminal: { error: unknown } | undefined;
  let closing = false;
  let finished = false;
  let iterating = false;
  const record = (error: unknown) => { terminal ??= { error }; };
  const abort = () => {
    if (finished || closing) return;
    record(options.signal!.reason);
    closing = true;
    parts.close();
  };
  options.signal?.addEventListener("abort", abort, { once: true });
  const closed = call<void>("plugin:watch.Watch", {root, recursive: options.recursive ?? false, debounceMs}, parts).then(
    () => { if (terminal) throw terminal.error; },
    (err: unknown) => { record(failure(err)); throw terminal!.error; },
  ).finally(() => {
    finished = true;
    options.signal?.removeEventListener("abort", abort);
  });
  void closed.catch(() => {});
  const checkPart = (part: Part) => {
    if (part.type === "error") {
      const error = codes.includes(part.code ?? "")
        ? new WatchError(part.code as WatchErrorCode, part.message ?? "filesystem watch stopped")
        : new Error("Invalid watch error response");
      record(error);
      return false;
    }
    return true;
  };
  const close = async () => {
    if (!closing && !finished) { closing = true; parts.close(); }
    await closed.catch(() => {});
  };
  try {
    const first = await stream.next();
    if (first.done) {
      await closed;
      throw new Error("Watch ended before ready");
    }
    if (!checkPart(first.value) || first.value.type !== "ready") {
      parts.close();
      await closed;
      throw new Error("Invalid watch ready response");
    }
    if (terminal) { await closed; throw terminal.error; }
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
            if (!checkPart(next.value)) { parts.close(); break; }
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
