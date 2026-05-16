// TinyJS Runtime — Service Worker Bridge
// Provides Go-callable Service Worker, Cache, Fetch, and SSE operations.

// --- Internal state ---

const _events = new Map();
let _nextEventId = 1;
const _caches = new Map();
let _nextCacheId = 1;
const _responses = new Map();
let _nextRespId = 1;
const _clients = new Map();
let _nextClientId = 1;
const _sseConns = new Map();
let _nextSseId = 1;

function _storeEvent(ev) {
  const id = _nextEventId++;
  _events.set(id, ev);
  return id;
}

function _storeResponse(resp) {
  if (!resp) return 0;
  const id = _nextRespId++;
  _responses.set(id, resp);
  return id;
}

function _storeClient(client) {
  const id = _nextClientId++;
  _clients.set(id, client);
  return id;
}

// --- Lifecycle ---
// Firefox requires all SW event listeners to be registered during the
// script's initial synchronous evaluation. Register them eagerly and
// queue events until Go's main() calls On*(fn).

let _goInstallHandler = null;
const _installQueue = [];
const _lifecycleResolvers = new Map();
self.addEventListener('install', (event) => {
  if (_goInstallHandler) {
    const id = _storeEvent(event);
    _goInstallHandler(id);
    _events.delete(id);
  } else {
    let resolve;
    event.waitUntil(new Promise((r) => { resolve = r; }));
    _installQueue.push({ event, resolve });
  }
});

export function OnInstall(fn) {
  _goInstallHandler = fn;
  while (_installQueue.length > 0) {
    const { event, resolve } = _installQueue.shift();
    const id = _storeEvent(event);
    _lifecycleResolvers.set(id, resolve);
    fn(id);
    _events.delete(id);
  }
}

let _goActivateHandler = null;
const _activateQueue = [];
self.addEventListener('activate', (event) => {
  if (_goActivateHandler) {
    const id = _storeEvent(event);
    _goActivateHandler(id);
    _events.delete(id);
  } else {
    let resolve;
    event.waitUntil(new Promise((r) => { resolve = r; }));
    _activateQueue.push({ event, resolve });
  }
});

export function OnActivate(fn) {
  _goActivateHandler = fn;
  while (_activateQueue.length > 0) {
    const { event, resolve } = _activateQueue.shift();
    const id = _storeEvent(event);
    _lifecycleResolvers.set(id, resolve);
    fn(id);
    _events.delete(id);
  }
}

// Fetch events use a deferred promise pattern:
// Go calls RespondWithCache or RespondWithNetwork synchronously to pick strategy.
// The runtime creates the promise and calls event.respondWith() synchronously.
const _fetchResolvers = new Map();

let _goFetchHandler = null;
self.addEventListener('fetch', (event) => {
  if (!_goFetchHandler) return; // no handler yet — let browser handle
  const id = _storeEvent(event);
  try {
    _goFetchHandler(id);
  } catch (e) {
    _events.delete(id);
    return;
  }
  const resolver = _fetchResolvers.get(id);
  if (resolver) {
    event.respondWith(resolver);
    _fetchResolvers.delete(id);
  }
  _events.delete(id);
});

export function OnFetch(fn) {
  _goFetchHandler = fn;
}

let _goMessageHandler = null;

// Early message queue — buffers messages arriving before main() registers OnMessage.
const _earlyQueue = [];
self.addEventListener('message', (event) => {
  if (_goMessageHandler) {
    const id = _storeEvent(event);
    try {
      _goMessageHandler(id);
    } catch (e) { /* runtime handles logging */ }
    _events.delete(id);
  } else {
    _earlyQueue.push(event);
  }
});

export function OnMessage(fn) {
  _goMessageHandler = fn;
  while (_earlyQueue.length > 0) {
    const id = _storeEvent(_earlyQueue.shift());
    try {
      fn(id);
    } catch (e) { /* runtime handles logging */ }
    _events.delete(id);
  }
}

// --- Event methods ---

export function WaitUntil(eventId, fn) {
  const resolver = _lifecycleResolvers.get(eventId);
  if (resolver) {
    _lifecycleResolvers.delete(eventId);
    fn(resolver);
    return;
  }
  const ev = _events.get(eventId);
  if (!ev) return;
  ev.waitUntil(new Promise((resolve) => {
    fn(resolve);
  }));
}

export function RespondWith(eventId, respId) {
  const resp = _responses.get(respId);
  if (resp) {
    _fetchResolvers.set(eventId, Promise.resolve(resp));
  }
}

export function RespondWithNetwork(eventId) {
  // Don't set a resolver — browser handles the fetch.
}

// RespondWithCacheFirst tries cache, falls back to network.
// This is the common pattern and must be called synchronously from onFetch.
export function RespondWithCacheFirst(eventId) {
  const ev = _events.get(eventId);
  if (!ev) return;
  _fetchResolvers.set(eventId,
    caches.match(ev.request).then(cached => cached || fetch(ev.request))
  );
}

// RespondWithProxyCache handles /proxy/<base64url> requests with a
// normalized cache key (the decoded upstream URL) so two different
// base64url encodings of the same upstream share one cache entry. Stamps
// each cached Response with X-Smesh-Cached-At for age-based prune. Cache
// hits younger than 30 days are returned; older or missing entries fall
// through to network and are re-cached on success.
const PROXY_CACHE_NAME = 'smesh-proxy';
const PROXY_CACHE_TTL_MS = 30 * 24 * 60 * 60 * 1000;
function _decodeProxyB64Url(s) {
  s = s.replace(/-/g, '+').replace(/_/g, '/');
  while (s.length % 4) s += '=';
  try { return atob(s); } catch (e) { return ''; }
}
function _proxyCacheKey(requestUrl) {
  const idx = requestUrl.indexOf('/proxy/');
  if (idx < 0) return requestUrl;
  const enc = requestUrl.substr(idx + '/proxy/'.length);
  const decoded = _decodeProxyB64Url(enc);
  if (!decoded) return requestUrl;
  return new URL('/__proxy/' + encodeURIComponent(decoded), requestUrl).toString();
}
async function _wrapWithCachedAt(resp) {
  const blob = await resp.clone().blob();
  const headers = new Headers(resp.headers);
  headers.set('X-Smesh-Cached-At', String(Date.now()));
  return new Response(blob, { status: resp.status, statusText: resp.statusText, headers });
}
export function RespondWithProxyCache(eventId) {
  const ev = _events.get(eventId);
  if (!ev) return;
  const key = _proxyCacheKey(ev.request.url);
  _fetchResolvers.set(eventId, (async () => {
    const cache = await caches.open(PROXY_CACHE_NAME);
    const cached = await cache.match(key);
    if (cached) {
      const stamp = Number(cached.headers.get('X-Smesh-Cached-At') || '0');
      if (stamp && Date.now() - stamp < PROXY_CACHE_TTL_MS) {
        return cached;
      }
    }
    const resp = await fetch(ev.request);
    if (resp.ok) {
      const wrapped = await _wrapWithCachedAt(resp.clone());
      cache.put(key, wrapped).catch(() => {});
    }
    return resp;
  })());
}
export function PruneProxyCache(maxAgeMs, fn) {
  (async () => {
    try {
      const cache = await caches.open(PROXY_CACHE_NAME);
      const keys = await cache.keys();
      const now = Date.now();
      for (const req of keys) {
        const resp = await cache.match(req);
        if (!resp) continue;
        const stamp = Number(resp.headers.get('X-Smesh-Cached-At') || '0');
        if (!stamp || now - stamp > maxAgeMs) {
          await cache.delete(req);
        }
      }
    } catch (e) {
      // best-effort
    }
    if (fn) fn();
  })();
}

// RespondWithNetworkFirst tries network, falls back to cache.
// Updates cache on successful fetch so offline still works.
export function RespondWithNetworkFirst(eventId) {
  const ev = _events.get(eventId);
  if (!ev) return;
  _fetchResolvers.set(eventId,
    fetch(ev.request).then(resp => {
      if (resp.ok) {
        const clone = resp.clone();
        caches.open('smesh').then(c => c.put(ev.request, clone));
      }
      return resp;
    }).catch(() => caches.match(ev.request).then(c => c || new Response('offline', {status: 503})))
  );
}

export function GetRequestURL(eventId) {
  const ev = _events.get(eventId);
  return ev ? ev.request.url : '';
}

export function GetRequestPath(eventId) {
  const ev = _events.get(eventId);
  if (!ev) return '';
  return new URL(ev.request.url).pathname;
}

export function GetMessageData(eventId) {
  const ev = _events.get(eventId);
  if (!ev) return '';
  const d = ev.data;
  return typeof d === 'string' ? d : JSON.stringify(d);
}

export function GetMessageClientID(eventId) {
  const ev = _events.get(eventId);
  if (!ev || !ev.source) return '';
  return ev.source.id || '';
}

// --- Resource cleanup ---

export function ReleaseResponse(respId) {
  _responses.delete(respId);
}

export function ReleaseClient(clientId) {
  _clients.delete(clientId);
}

// --- SW globals ---

export function SkipWaiting() {
  self.skipWaiting();
}

export function ClaimClients(done) {
  self.clients.claim().then(() => { if (done) done(); });
}

export function MatchClients(fn) {
  self.clients.matchAll({ type: 'window' }).then((all) => {
    for (const c of all) {
      fn(_storeClient(c));
    }
  });
}

export function PostMessage(clientId, msg) {
  const c = _clients.get(clientId);
  if (c) c.postMessage({ type: '' + msg });
}

export function PostMessageJSON(clientId, json) {
  const c = _clients.get(clientId);
  if (c) c.postMessage('' + json);
}

export function GetClientByID(id, fn) {
  self.clients.get(id).then((client) => {
    if (client) {
      fn(_storeClient(client), true);
    } else {
      fn(0, false);
    }
  });
}

export function Navigate(clientId, url) {
  const c = _clients.get(clientId);
  if (c) c.navigate(url || c.url);
}

// --- Cache ---

export function CacheOpen(name, fn) {
  caches.open(name).then((cache) => {
    const id = _nextCacheId++;
    _caches.set(id, cache);
    fn(id);
  }).catch(() => { fn(0); });
}

export function CacheAddAll(cacheId, urls, done) {
  const cache = _caches.get(cacheId);
  if (!cache) { if (done) done(); return; }
  // Add each URL individually, skipping failures.
  Promise.allSettled(urls.map(u =>
    fetch(u).then(r => r.ok ? cache.put(u, r) : null).catch(() => {})
  )).then(() => { if (done) done(); });
}

export function CacheFromManifests(cacheId, staticFiles, done) {
  const cache = _caches.get(cacheId);
  if (!cache) { if (done) done(); return; }
  // Bypass HTTP cache when fetching manifests and assets — otherwise the
  // browser may serve stale files, defeating the whole point of refresh.
  const noCache = { cache: 'reload' };
  Promise.all([
    fetch('/$manifest.json', noCache).then(r => r.ok ? r.json() : []).catch(() => []),
    fetch('/$sw/$manifest.json', noCache).then(r => r.ok ? r.json() : []).catch(() => []),
  ]).then(([app, sw]) => {
    const urls = new Set();
    // Static files from Moxie source.
    if (staticFiles && staticFiles.forEach) {
      staticFiles.forEach(f => urls.add(f));
    } else if (staticFiles && staticFiles.$get) {
      const s = staticFiles.$get();
      for (let i = 0; i < s.length; i++) urls.add(s.addr ? s.addr(i).$get() : s[i]);
    }
    // App build outputs.
    for (const f of app) urls.add('/' + f);
    // SW build outputs.
    for (const f of sw) urls.add('/$sw/' + f);
    // Cache all, skipping failures. Use cache: 'reload' to bypass HTTP cache.
    Promise.allSettled([...urls].map(u =>
      fetch(u, noCache).then(r => r.ok ? cache.put(u, r) : null).catch(() => {})
    )).then(() => { if (done) done(); });
  });
}

export function CachePut(cacheId, url, respId, done) {
  const cache = _caches.get(cacheId);
  const resp = _responses.get(respId);
  if (!cache || !resp) { if (done) done(); return; }
  cache.put(new Request(url), resp).then(() => { if (done) done(); });
}

export function CacheMatch(url, fn) {
  caches.match(new Request(url)).then((resp) => {
    fn(_storeResponse(resp));
  });
}

export function CacheDelete(name, done) {
  caches.delete(name).then(() => { if (done) done(); });
}

// --- Fetch ---

export function Fetch(url, fn) {
  fetch(url).then(
    (resp) => fn(_storeResponse(resp), true),
    () => fn(0, false)
  );
}

export function FetchAll(urls, onEach, onDone) {
  let remaining = urls.length;
  if (remaining === 0) { if (onDone) onDone(); return; }
  urls.forEach((url, i) => {
    fetch(url).then(
      (resp) => { onEach(i, _storeResponse(resp), true); },
      () => { onEach(i, 0, false); }
    ).finally(() => {
      remaining--;
      if (remaining === 0 && onDone) onDone();
    });
  });
}

export function ResponseOK(respId) {
  const resp = _responses.get(respId);
  return resp ? resp.ok : false;
}

// --- SSE ---

export function SSEConnect(url, onMessage) {
  if (typeof EventSource === 'undefined') {
    // EventSource is not available in Service Worker scope.
    // Poll instead. Bypass HTTP cache so a stale 304 doesn't mask updates.
    const id = _nextSseId++;
    let last = '';
    function poll() {
      fetch(url, { cache: 'reload' }).then(r => r.ok ? r.text() : '').then(t => {
        if (t && t !== last) { last = t; if (onMessage) onMessage(t.replace(/^data:\s*/, '')); }
      }).catch(() => {});
      setTimeout(poll, 3000);
    }
    setTimeout(poll, 500);
    return id;
  }
  const id = _nextSseId++;
  const es = new EventSource(url);
  _sseConns.set(id, es);
  es.onmessage = (event) => {
    if (onMessage) onMessage(event.data);
  };
  return id;
}

export function SSEClose(sseId) {
  const es = _sseConns.get(sseId);
  if (es) {
    es.close();
    _sseConns.delete(sseId);
  }
}

// --- Timers ---

export function SetTimeout(ms, fn) {
  return setTimeout(fn, ms);
}

export function ClearTimeout(timerId) {
  clearTimeout(timerId);
}

// --- Time ---

export function NowSeconds() {
  return BigInt(Math.floor(Date.now() / 1000));
}

export function NowMillis() {
  return BigInt(Date.now());
}

// --- SW globals ---

export function Origin() {
  return self.location.origin;
}

// --- Logging ---

export function Log(msg) {
  console.log('sw:', msg);
}

