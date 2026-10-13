// sw-host.mjs — the service worker's half of a Moxie wasm bundle.
//
// A service worker is a third execution context: no document, no Worker, its
// own global with the lifecycle events. What it shares with the Worker host is
// everything that matters - the WASI/env import object, the channel and spawn
// bridge, the string and callback marshalling, the IndexedDB imports - so this
// host is that plus the sw_* entries and nothing else. It is copied to
// $sw/wasm-host-sw.mjs by `moxie build -target wasm`, beside $runtime/, which
// is why it imports through ../$runtime/ (the same specifier resolves from
// inside $runtime/ too, where the runtime copy also lands).
//
// There is no DOM relay in this context, so a DOM call from a service worker
// fails by name: a worker has no document to reach, and pretending otherwise
// would be the silent nothing this framework exists to remove.
import * as sw from '../$runtime/sw.mjs';
import {
  buildHostImports, buildWorkerBridge, buildIdbBridge, setHostModule,
  readStr, splitNull, setOutStr,
  cb0, cbs, cbb, cbi, cbii, cbiii,
} from '../$runtime/host.mjs';
import { makeDomProxy } from '../$runtime/relay.mjs';

let memory;
const wasmUrl = new URL('./sw.wasm', import.meta.url).href;

const bridge = {
  // --- lifecycle ---
  sw_on_install(cbID) { sw.OnInstall((id) => cbi(cbID, id)); },
  sw_on_activate(cbID) { sw.OnActivate((id) => cbi(cbID, id)); },
  sw_on_fetch(cbID) { sw.OnFetch((id) => cbi(cbID, id)); },
  sw_on_message(cbID) { sw.OnMessage((id) => cbi(cbID, id)); },
  // The module's handler receives a "done" function and the page side waits on
  // the promise it resolves: a service worker is killed as soon as its event
  // handler settles, so extension is the handler's own decision.
  sw_wait_until(event, cbID) { sw.WaitUntil(event, (done) => { cb0(cbID); done(); }); },

  // --- fetch events ---
  sw_respond_with(event, resp) { sw.RespondWith(event, resp); },
  sw_respond_with_network(event) { sw.RespondWithNetwork(event); },
  sw_respond_with_cache_first(event) { sw.RespondWithCacheFirst(event); },
  sw_respond_with_network_first(event) { sw.RespondWithNetworkFirst(event); },
  sw_respond_with_proxy_cache(event) { sw.RespondWithProxyCache(event); },
  sw_prune_proxy_cache(maxAgeMs, cbID) { sw.PruneProxyCache(maxAgeMs, () => cb0(cbID)); },
  sw_get_request_url(event, outPtr, outLen) { setOutStr(outPtr, outLen, sw.GetRequestURL(event)); },
  sw_get_request_path(event, outPtr, outLen) { setOutStr(outPtr, outLen, sw.GetRequestPath(event)); },
  sw_get_message_data(event, outPtr, outLen) { setOutStr(outPtr, outLen, sw.GetMessageData(event)); },
  sw_get_message_client_id(event, outPtr, outLen) {
    setOutStr(outPtr, outLen, sw.GetMessageClientID(event));
  },

  // --- responses and clients ---
  sw_response_ok(resp) { return sw.ResponseOK(resp) ? 1 : 0; },
  sw_skip_waiting() { sw.SkipWaiting(); },
  sw_claim_clients(cbID) { sw.ClaimClients(() => cb0(cbID)); },
  sw_match_clients(cbID) { sw.MatchClients((client) => cbi(cbID, client)); },
  sw_post_message(client, ptr, len, _c) { sw.PostMessage(client, readStr(ptr, len)); },
  sw_post_message_json(client, ptr, len, _c) { sw.PostMessageJSON(client, readStr(ptr, len)); },
  sw_get_client_by_id(ptr, len, _c, cbID) {
    sw.GetClientByID(readStr(ptr, len), (client, found) => cbii(cbID, client, found ? 1 : 0));
  },
  sw_navigate(client, ptr, len, _c) { sw.Navigate(client, readStr(ptr, len)); },
  sw_origin(outPtr, outLen) { setOutStr(outPtr, outLen, sw.Origin()); },
  sw_log(ptr, len, _c) { sw.Log(readStr(ptr, len)); },

  // --- cache ---
  sw_cache_open(ptr, len, _c, cbID) { sw.CacheOpen(readStr(ptr, len), (cache) => cbi(cbID, cache)); },
  // The url lists arrive NUL-separated with an explicit count, the same shape
  // the page-side splitNull helper reads.
  sw_cache_add_all(cache, urlsPtr, urlsLen, _uc, count, cbID) {
    sw.CacheAddAll(cache, splitNull(urlsPtr, urlsLen, count), () => cb0(cbID));
  },
  sw_cache_from_manifests(cache, filesPtr, filesLen, _fc, count, cbID) {
    sw.CacheFromManifests(cache, splitNull(filesPtr, filesLen, count), () => cb0(cbID));
  },
  sw_cache_put(cache, urlPtr, urlLen, _uc, resp, cbID) {
    sw.CachePut(cache, readStr(urlPtr, urlLen), resp, () => cb0(cbID));
  },
  sw_cache_match(ptr, len, _c, cbID) { sw.CacheMatch(readStr(ptr, len), (resp) => cbi(cbID, resp)); },
  sw_cache_delete(ptr, len, _c, cbID) { sw.CacheDelete(readStr(ptr, len), () => cb0(cbID)); },

  // --- network ---
  sw_fetch(ptr, len, _c, cbID) {
    sw.Fetch(readStr(ptr, len), (resp, ok) => cbii(cbID, resp, ok ? 1 : 0));
  },
  sw_fetch_all(urlsPtr, urlsLen, _uc, count, eachID, doneID) {
    sw.FetchAll(splitNull(urlsPtr, urlsLen, count),
      (idx, resp, ok) => cbiii(eachID, idx, resp, ok ? 1 : 0),
      () => cb0(doneID));
  },
  sw_sse_connect(ptr, len, _c, cbID) {
    return sw.SSEConnect(readStr(ptr, len), (data) => cbs(cbID, data));
  },
  sw_sse_close(id) { sw.SSEClose(id); },

  // --- timers and time ---
  sw_set_timeout(ms, cbID) { return sw.SetTimeout(ms, () => cb0(cbID)); },
  sw_clear_timeout(timer) { sw.ClearTimeout(timer); },
  sw_now_seconds() { return sw.NowSeconds(); },
  sw_now_millis() { return sw.NowMillis(); },

  // --- storage: the same IndexedDB imports a page module gets ---
  ...buildIdbBridge(),
};

fetch(wasmUrl)
  .then((r) => r.arrayBuffer())
  .then((bytes) => WebAssembly.instantiate(bytes, {
    ...buildHostImports(() => memory),
    bridge: { ...buildWorkerBridge(new Map(), makeDomProxy(null)), ...bridge },
  }))
  .then(({ instance }) => {
    memory = instance.exports.memory;
    setHostModule(instance);
    instance.exports._start();
    // The module has had its chance to register handlers; anything still queued
    // is an event this worker does not serve, and leaving it pending would keep
    // the worker in "activating" for good.
    sw.ModuleReady();
    // This worker is the bundle's worker: there is never a reason to keep the
    // previous generation in charge of a page that just loaded the new one, and
    // an install that waits for every old client to go away is an upgrade that
    // may not happen until the next visit.
    self.skipWaiting();
  })
  .catch((e) => {
    // A service worker that fails to boot must say so where a developer can
    // see it: there is no page to report to, and silence here would look like
    // a worker that simply has nothing to do.
    console.error('[sw] wasm boot failed: ' + String((e && e.stack) || e));
  });
