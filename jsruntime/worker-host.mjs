// worker-host.mjs - a Moxie domain running in a dedicated Worker.
//
// The module never runs on the page thread: channel_recv uses Atomics.wait,
// which the main browser thread forbids, and spawn() needs the page to start
// its domains. Two kinds of work arrive here over the module's own port:
//
//   {kind:'root', wasm}                     the application itself: _start()
//   {kind:'entry', wasm, fnIdx, args, chans} a spawned domain: __spawn_entry
//
// Either way the module is instantiated with is_worker_context() = 1 and one
// supervisor port to the page's hub. Control (spawn/exit/panic/heartbeat) and
// logs go over that port; the channel rings do not - they are the data plane
// and never pass through the page. See docs/wasm-hub.md.
import { buildHostImports, buildWorkerBridge, setHostModule, dispatchCallback } from './host.mjs';
import { makeDomProxy } from './relay.mjs';

let memory;
let domainId = '1';
let domainParent = '0';
let supervisor = null;

function post(msg) {
  if (!supervisor) return;
  try { supervisor.postMessage(msg); } catch (e) { /* the hub is gone */ }
}

// Every write the module makes is a log-plane message to the hub. It is never
// forwarded up a tree: a parent that forwards is a parent parked in a receive,
// and a parked domain cannot pump anything.
function output(fd, text) {
  post({ kind: 'log', id: domainId, fd, text });
}

function finish() {
  post({ kind: 'exit', id: domainId });
}

function fail(e) {
  post({ kind: 'panic', id: domainId, text: String((e && e.stack) || e) });
}

// A domain that ends by calling os.Exit(0) finished; it did not fail. The host
// has no process to end, so syscall.Exit reports itself by throwing, and the
// exit code is carried on the thrown object.
function settleRun(e) {
  if (e && e.moxieExit === 0) { finish(); return; }
  fail(e);
}

// Messages the hub sends to a domain: only a re-parent (an intermediate above
// it exited cleanly) and a refused spawn.
function onSupervisorMessage(e) {
  const m = e.data;
  if (!m) return;
  if (m.kind === 'rebind') { domainParent = String(m.parent); return; }
  if (m.kind === 'refused') {
    output(2, 'spawn refused (' + String(m.reason) + '): no domain was started\n');
  }
}

// bindChannels turns the parent's handle/SAB pairs into this domain's channel
// table, so channel_send/recv/close address the far end of the same rings.
function bindChannels(list) {
  const channels = new Map();
  for (const c of list || []) channels.set(c.handle, c.sab);
  return channels;
}

async function instantiate(wasmPath, channels, relayPort, appBridgeUrl, appPort) {
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
    ...buildHostImports(() => memory, { onOutput: output }),
    bridge: { ...buildWorkerBridge(channels, makeDomProxy(relayPort), supervisor, domainId, domainParent), ...appBridge },
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

function runRoot(wasmPath, relayPort, appBridgeUrl, appPort) {
  // A spawned domain is started from THIS realm, and its module URL comes from
  // here: without this the child's boot message carried `wasm: undefined`, it
  // fetched a 404 page and instantiated HTML.
  globalThis.__moxie_wasm_url = wasmPath;
  instantiate(wasmPath, new Map(), relayPort, appBridgeUrl, appPort).then((inst) => {
    setHostModule(inst);
    // The supervisor requirement is a property of the MODULE, so it is checked
    // when the module boots, not when it first spawns. A build that has
    // __spawn_entry was built with spawn support; booting it without a
    // supervisor means its first spawn() would have nowhere to go.
    if (typeof inst.exports.__spawn_entry === 'function' && !supervisor) {
      throw new Error('this module was built with spawn support but was booted without a supervisor port; wasm spawn needs the page-supervised host (jsruntime/loader.mjs or startBundle)');
    }
    serveCallbacks(relayPort);
    inst.exports._start();
    finish();
  }).catch(settleRun);
}

function runEntry(wasmPath, fnIdx, args, chanList, breaker) {
  // A spawned domain that spawns again reads this in the bridge, exactly as the
  // root does.
  globalThis.__moxie_wasm_url = wasmPath;
  if (!supervisor) throw new Error('a spawned wasm domain needs a supervisor port to the page');
  // The breaker is a channel the child must be able to receive on and close,
  // so it is bound into the same table the parameters use.
  const bound = (chanList || []).slice();
  if (breaker) bound.push(breaker);
  instantiate(wasmPath, bindChannels(bound), null, null, null).then((inst) => {
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
    finish();
  }).catch(settleRun);
}

globalThis.addEventListener('message', (e) => {
  const m = e.data;
  if (!m) return;
  // The test host asks for load-only stubs (the gojs namespace); a production
  // page never sets this, so a module that needs the real bridge still fails
  // loudly there.
  globalThis.__moxie_test_stubs = m.stubs === true;
  supervisor = m.supervisorPort || null;
  domainId = typeof m.id === 'string' ? m.id : '1';
  domainParent = typeof m.parent === 'string' ? m.parent : '0';
  if (supervisor) {
    supervisor.addEventListener('message', onSupervisorMessage);
    supervisor.start();
  }
  if (m.kind === 'root') runRoot(m.wasm, m.relayPort || null, m.appBridge || null, m.appPort || null);
  else if (m.kind === 'entry') runEntry(m.wasm, m.fnIdx, m.args || new Uint8Array(0), m.chans, m.breaker || null);
});
