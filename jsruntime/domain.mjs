// domain.mjs — Domain manager for spawn in the Moxie JS runtime.
//
// moxiejs path (JS compiled output): spawn() is a cooperative microtask domain.
// wasm path: spawn() emits bridge.spawn_domain() which creates a real Worker
//   via worker-host.mjs, started by the page, using SAB channels
//   (see sab-channel.mjs and host.mjs).
//   The compiler assigns fnIdx at compile time; __spawn_entry in the Worker
//   dispatches via __spawn_dispatch[fnIdx].
//
// This file serves the moxiejs path only. True OS-process isolation
// (matching the native spawn() semantics) requires the wasm target.

let domainCounter = 0;

export function spawn(asyncFn) {
  const id = ++domainCounter;
  queueMicrotask(async () => {
    try {
      await asyncFn();
    } catch (e) {
      console.error('[domain ' + id + '] panic:', e);
    }
  });
}
