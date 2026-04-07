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

export function OnInstall(fn) {
  self.addEventListener('install', (event) => {
    const id = _storeEvent(event);
    fn(id);
    _events.delete(id);
  });
}

export function OnActivate(fn) {
  self.addEventListener('activate', (event) => {
    const id = _storeEvent(event);
    fn(id);
    _events.delete(id);
  });
}

// Fetch events use a deferred promise pattern:
// Go calls RespondWithCache or RespondWithNetwork synchronously to pick strategy.
// The runtime creates the promise and calls event.respondWith() synchronously.
const _fetchResolvers = new Map();

export function OnFetch(fn) {
  self.addEventListener('fetch', (event) => {
    const id = _storeEvent(event);
    fn(id);
    // Check if Go code set up a response strategy.
    const resolver = _fetchResolvers.get(id);
    if (resolver) {
      event.respondWith(resolver);
      _fetchResolvers.delete(id);
    }
    // Go handler read what it needed synchronously — release the event.
    _events.delete(id);
  });
}

let _goMessageHandler = null;

// Early message queue — buffers messages arriving before main() registers OnMessage.
const _earlyQueue = [];
self.addEventListener('message', (event) => {
  if (_goMessageHandler) {
    const id = _storeEvent(event);
    _goMessageHandler(id);
    _events.delete(id);
  } else {
    _earlyQueue.push(event);
  }
});

export function OnMessage(fn) {
  _goMessageHandler = fn;
  while (_earlyQueue.length > 0) {
    const id = _storeEvent(_earlyQueue.shift());
    fn(id);
    _events.delete(id);
  }
}

// --- Event methods ---

export function WaitUntil(eventId, fn) {
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
  if (c) c.postMessage({ type: msg });
}

export function PostMessageJSON(clientId, json) {
  const c = _clients.get(clientId);
  if (c) c.postMessage(json);
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

