// worker-host.mjs — a Moxie domain running in a dedicated Worker.
//
// The module never runs on the page thread: channel_recv uses Atomics.wait,
// which the main browser thread forbids, and spawn() creates a new Worker
// from inside one. Two kinds of work arrive here over the same message port:
//
//   {kind:'root', wasm}                     the application itself: _start()
//   {kind:'entry', wasm, fnIdx, args, chans} a spawned domain: __spawn_entry
//
// Either way the module is instantiated with is_worker_context() = 1, and a
// spawned domain is given the channel handles the parent created so the child
// owns the far end of the same SPSC rings. Nothing else crosses the boundary.
import { buildHostImports, buildWorkerBridge, setHostModule } from './host.mjs';

let memory;

globalThis.addEventListener('error', (e) => {
  globalThis.postMessage({ kind: 'error', error: String((e && e.error && e.error.stack) || e.message) });
});
globalThis.addEventListener('unhandledrejection', (e) => {
  globalThis.postMessage({ kind: 'error', error: String((e.reason && e.reason.stack) || e.reason) });
});

// bindChannels turns the parent's handle/SAB pairs into this domain's channel
// table, so channel_send/recv/close address the far end of the same rings.
function bindChannels(list) {
  const channels = new Map();
  for (const c of list || []) channels.set(c.handle, c.sab);
  return channels;
}

async function instantiate(wasmPath, channels) {
  const response = await fetch(wasmPath);
  const bytes = await response.arrayBuffer();
  const result = await WebAssembly.instantiate(bytes, {
    ...buildHostImports(() => memory),
    bridge: buildWorkerBridge(channels),
  });
  memory = result.instance.exports.memory;
  return result.instance;
}

function runRoot(wasmPath) {
  instantiate(wasmPath, new Map()).then((inst) => {
    setHostModule(inst);
    inst.exports._start();
    globalThis.postMessage({ kind: 'done' });
  }).catch((e) => {
    globalThis.postMessage({ kind: 'error', error: String((e && e.stack) || e) });
  });
}

function runEntry(wasmPath, fnIdx, args, chanList) {
  instantiate(wasmPath, bindChannels(chanList)).then((inst) => {
    setHostModule(inst);
    if (typeof inst.exports.__spawn_entry !== 'function') {
      throw new Error('module has no __spawn_entry export; it was not built with spawn support');
    }
    // The compiler packs scalar args into linear memory; __spawn_entry reads
    // them from there, and channel params arrive as int32 handles.
    const ptr = inst.exports.__alloc(args.length);
    new Uint8Array(memory.buffer, ptr, args.length).set(args);
    const dv = new DataView(memory.buffer);
    const handles = (chanList || []).map(c => c.handle);
    const hptr = inst.exports.__alloc(handles.length * 4);
    handles.forEach((h, i) => dv.setInt32(hptr + i * 4, h, true));
    inst.exports.__spawn_entry(fnIdx, ptr, args.length, hptr, handles.length);
    globalThis.postMessage({ kind: 'done' });
  }).catch((e) => {
    globalThis.postMessage({ kind: 'error', error: String((e && e.stack) || e) });
  });
}

globalThis.addEventListener('message', (e) => {
  const m = e.data;
  if (!m) return;
  // The test host asks for load-only stubs (the gojs namespace); a production
  // page never sets this, so a module that needs the real bridge still fails
  // loudly there.
  globalThis.__moxie_test_stubs = m.stubs === true;
  if (m.kind === 'root') runRoot(m.wasm);
  else if (m.kind === 'entry') runEntry(m.wasm, m.fnIdx, m.args || new Uint8Array(0), m.chans);
});
