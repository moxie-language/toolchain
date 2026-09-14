// WASM Loader — the page side of a Moxie wasm application.
//
// The module never runs on the page thread: load() starts the root domain in
// jsruntime/worker-host.mjs and resolves when it returns. The host functions
// the module imports (bridge, WASI, env) live in jsruntime/host.mjs.

import { startDomain } from './host.mjs';

export function load(wasmPath) {
  globalThis.__moxie_wasm_url = wasmPath;
  return new Promise((resolve, reject) => {
    startDomain({ kind: 'root', wasm: wasmPath }, (err) => err ? reject(err) : resolve());
  });
}
