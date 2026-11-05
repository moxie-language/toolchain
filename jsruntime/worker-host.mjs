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
import { buildHostImports, buildWorkerBridge, setHostModule, dispatchCallback } from './host.mjs';
import { makeDomProxy } from './relay.mjs';

let memory;

// A domain that ends by calling os.Exit(0) finished; it did not fail. The
// host has no process to end, so syscall.Exit reports itself by throwing, and
// the exit code is carried on the thrown object. Without this a clean exit is
// posted as an error and the module's whole output is discarded - the fixture
// that calls os.Exit(0) at the end of main looked like a crash.
function settled(e, onFailure) {
  if (e && e.moxieExit === 0) {
    globalThis.postMessage({ kind: 'done' });
    return;
  }
  onFailure();
}

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

async function instantiate(wasmPath, channels, relayPort, appBridgeUrl, appPort, supervisor) {
  const response = await fetch(wasmPath);
  const bytes = await response.arrayBuffer();
  // The application's own bridge entries, imported here because a function
  // cannot cross a postMessage boundary. A URL that does not load is a build or
  // deployment error, not something to swallow: the module would otherwise
  // instantiate with entries missing and fail later, somewhere less obvious.
  let appBridge = {};
  if (appBridgeUrl) {
    const mod = await import(appBridgeUrl);
    const factory = mod.default || mod;
    // A bridge that needs the page port (a worker bundle that answers messages)
    // is a factory taking it; a bridge that only answers imports is an object.
    appBridge = typeof factory === 'function' ? factory({ port: appPort || null }) : factory;
  }
  const result = await WebAssembly.instantiate(bytes, {
    ...buildHostImports(() => memory),
    bridge: { ...buildWorkerBridge(channels, makeDomProxy(relayPort), supervisor), ...appBridge },
  });
  memory = result.instance.exports.memory;
  return result.instance;
}

// serveCallbacks delivers the answers the page produces after a call returned:
// an event, a timer, a fetch reply. The module is idle in its event loop by
// then - it returned from _start and the page owns it - so each callback is a
// fresh entry into wasm, which is why they are queued rather than called from
// this handler's stack.
function serveCallbacks(port) {
  if (!port) return;
  port.addEventListener('message', (e) => {
    const m = e.data;
    if (m && typeof m.cb === 'string') dispatchCallback(m);
  });
  port.start();
}

function runRoot(wasmPath, relayPort, appBridgeUrl, appPort, supervisor) {
  // A spawned domain is started from THIS realm (spawnWorker reads
  // globalThis.__moxie_wasm_url), and the page's loader set that only in the
  // page's own realm. Without this the child's boot message carried
  // `wasm: undefined`, it fetched a 404 page and instantiated HTML.
  globalThis.__moxie_wasm_url = wasmPath;
  instantiate(wasmPath, new Map(), relayPort, appBridgeUrl, appPort, supervisor).then((inst) => {
    setHostModule(inst);
    serveCallbacks(relayPort);
    inst.exports._start();
    globalThis.postMessage({ kind: 'done' });
  }).catch((e) => {
    settled(e, () => globalThis.postMessage({ kind: 'error', error: String((e && e.stack) || e) }));
  });
}

function runEntry(wasmPath, fnIdx, args, chanList, supervisor, breaker) {
  // A spawned domain that spawns again reads this in spawnWorker, exactly as
  // the root does.
  globalThis.__moxie_wasm_url = wasmPath;
  // A spawned domain holds no page connection: the document is reachable only
  // from the domain the page handed a relay port to.
  // The breaker is a channel the child must be able to receive on and close,
  // so it is bound into the same table the parameters use.
  const bound = (chanList || []).slice();
  if (breaker) bound.push(breaker);
  instantiate(wasmPath, bindChannels(bound), null, null, null, supervisor).then((inst) => {
    setHostModule(inst);
    if (typeof inst.exports.__spawn_entry !== 'function') {
      throw new Error('module has no __spawn_entry export; it was not built with spawn support');
    }
    // The compiler packs scalar args into linear memory; __spawn_entry reads
    // them from there, and channel params arrive as int32 handles. The staging
    // area is STATIC (__spawn_scratch), not __alloc: this Worker has just
    // instantiated the module and the domain does not exist until
    // __spawn_entry initializes it, so there is no arena to allocate from.
    const handles = (chanList || []).map(c => c.handle);
    const need = args.length + handles.length * 4;
    const cap = inst.exports.__spawn_scratch_size();
    if (need > cap) {
      throw new Error('spawn arguments exceed the wasm staging area: ' + need + ' > ' + cap);
    }
    const ptr = inst.exports.__spawn_scratch();
    new Uint8Array(memory.buffer, ptr, args.length).set(args);
    const hptr = ptr + ((args.length + 3) & ~3);
    const dv = new DataView(memory.buffer);
    handles.forEach((h, i) => dv.setInt32(hptr + i * 4, h, true));
    inst.exports.__spawn_entry(fnIdx, ptr, args.length, hptr, handles.length, breaker ? breaker.handle : 0);
    globalThis.postMessage({ kind: 'done' });
  }).catch((e) => {
    settled(e, () => globalThis.postMessage({ kind: 'error', error: String((e && e.stack) || e) }));
  });
}

globalThis.addEventListener('message', (e) => {
  const m = e.data;
  if (!m) return;
  // The test host asks for load-only stubs (the gojs namespace); a production
  // page never sets this, so a module that needs the real bridge still fails
  // loudly there.
  globalThis.__moxie_test_stubs = m.stubs === true;
  if (m.kind === 'root') runRoot(m.wasm, m.relayPort || null, m.appBridge || null, m.appPort || null, m.supervisorPort || null);
  else if (m.kind === 'entry') runEntry(m.wasm, m.fnIdx, m.args || new Uint8Array(0), m.chans, m.supervisorPort || null, m.breaker || null);
});
