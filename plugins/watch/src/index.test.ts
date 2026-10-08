import { afterEach, expect, test } from "bun:test";
import { fakeGo } from "../../fake-go";
import { watch, WatchError } from "./index";
let cleanup = () => {};
afterEach(() => cleanup());
test("ready, flatten events, and explicit close waits for Go cleanup", async () => {
 let cleaned = false;
 const go = fakeGo({ "plugin:watch.Watch": async ({send,signal,args}) => {
  expect(args[0]).toEqual({root:"project",recursive:false,debounceMs:50});
  send(1,{type:"ready"});
  send(1,{type:"events",events:[{op:"create",path:"a",isDir:false},{op:"write",path:"a",isDir:false}]});
  await new Promise<void>(r => signal.addEventListener("abort", () => r(),{once:true}));
  cleaned=true;
 }}); cleanup=go.uninstall;
 const w = await watch("project");
 const iterator = w[Symbol.asyncIterator]();
 expect((await iterator.next()).value?.op).toBe("create");
 expect((await iterator.next()).value?.op).toBe("write");
 await w.close(); await w.close(); expect(cleaned).toBe(true);
});
test("sanitized final call failure survives channel end", async () => {
 const go=fakeGo({"plugin:watch.Watch":({send})=>{send(1,{type:"ready"});throw new Error("watch:overflow:filesystem watch overflow");}});cleanup=go.uninstall;
 const w=await watch("project");
 await expect(w.closed).rejects.toBeInstanceOf(WatchError);
 await expect(w.close()).resolves.toBeUndefined();
 await expect(w.close()).resolves.toBeUndefined();
 await expect(w.closed).rejects.toBeInstanceOf(WatchError);
});
test("invalid options and already aborted signal never dispatch", async()=>{
 const go=fakeGo({});cleanup=go.uninstall;
 await expect(watch("project",{debounceMs:NaN})).rejects.toBeInstanceOf(TypeError);
 const signal=AbortSignal.abort("stop");
 await expect(watch("project",{signal})).rejects.toBe("stop");
 expect(go.calls).toHaveLength(0);
});
