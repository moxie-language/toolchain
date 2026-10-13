// WASM Loader — the page side of a Moxie wasm application.
//
// The module never runs on the page thread: load() starts the root domain in
// jsruntime/worker-host.mjs and resolves when it returns. The host functions
// the module imports (bridge, WASI, env) live in jsruntime/host.mjs.

import { startDomain } from './host.mjs';

// load starts a bundle's root domain and resolves when its main returns.
//
// opts.appBridge names a module whose default export is merged into the bridge:
// a bundle may import symbols only its own application can answer (a store
// worker's message loop, an extension's storage, a protocol shim), and the
// bridge functions cannot be structured-cloned from the page to the Worker, so
// the Worker imports the module itself. Hosts stay framework; bridges stay app.
export function load(wasmPath, opts = {}) {
  globalThis.__moxie_wasm_url = wasmPath;
  // A relative bridge path means "beside the bundle": resolve it here, because
  // the Worker is what imports it and the Worker's own URL is in $runtime/.
  const appBridge = opts.appBridge
    ? new URL(opts.appBridge, new URL(wasmPath, globalThis.location.href)).href
    : null;
  return new Promise((resolve, reject) => {
    startDomain({ kind: 'root', wasm: wasmPath, appBridge },
      (err) => err ? reject(err) : resolve());
  });
}
