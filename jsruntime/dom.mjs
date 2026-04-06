// TinyJS Runtime — DOM Bridge
// Provides Go-callable browser DOM operations.
// Elements are tracked by integer handles.
// Function names match Go signatures (PascalCase).

const _elements = new Map();
let _nextId = 1; // Safe up to 2^53 in JS. No wrap needed.
const _callbacks = new Map();
let _nextCb = 1;

// Bootstrap: map handle 0 to document.body (available after DOMContentLoaded).
function ensureBody() {
  if (!_elements.has(0) && typeof document !== 'undefined' && document.body) {
    _elements.set(0, document.body);
  }
}

// Element creation and lookup.
export function CreateElement(tag) {
  const el = document.createElement(tag);
  const id = _nextId++;
  _elements.set(id, el);
  return id;
}

export function CreateTextNode(text) {
  const node = document.createTextNode(text);
  const id = _nextId++;
  _elements.set(id, node);
  return id;
}

export function GetElementById(id) {
  const el = document.getElementById(id);
  if (!el) return 0;
  const handle = _nextId++;
  _elements.set(handle, el);
  return handle;
}

export function QuerySelector(sel) {
  const el = document.querySelector(sel);
  if (!el) return 0;
  const handle = _nextId++;
  _elements.set(handle, el);
  return handle;
}

export function Body() {
  ensureBody();
  return 0;
}

// Tree manipulation.
export function AppendChild(parentId, childId) {
  const parent = _elements.get(parentId);
  const child = _elements.get(childId);
  if (parent && child) parent.appendChild(child);
}

export function RemoveChild(parentId, childId) {
  const parent = _elements.get(parentId);
  const child = _elements.get(childId);
  if (parent && child) parent.removeChild(child);
}

export function FirstChild(parentId) {
  const parent = _elements.get(parentId);
  if (parent && parent.firstChild) {
    const id = _nextId++;
    _elements.set(id, parent.firstChild);
    return id;
  }
  return 0;
}

export function InsertBefore(parentId, newId, refId) {
  const parent = _elements.get(parentId);
  const newEl = _elements.get(newId);
  const ref = refId >= 0 ? _elements.get(refId) : null;
  if (parent && newEl) parent.insertBefore(newEl, ref);
}

export function ReplaceChild(parentId, newId, oldId) {
  const parent = _elements.get(parentId);
  const newEl = _elements.get(newId);
  const oldEl = _elements.get(oldId);
  if (parent && newEl && oldEl) parent.replaceChild(newEl, oldEl);
}

// Properties and attributes.
export function SetAttribute(elId, name, value) {
  const el = _elements.get(elId);
  if (el) el.setAttribute(name, value);
}

export function RemoveAttribute(elId, name) {
  const el = _elements.get(elId);
  if (el) el.removeAttribute(name);
}

export function SetTextContent(elId, text) {
  const el = _elements.get(elId);
  if (el) el.textContent = text;
}

export function SetInnerHTML(elId, html) {
  const el = _elements.get(elId);
  if (el) el.innerHTML = html;
}

export function SetStyle(elId, prop, value) {
  const el = _elements.get(elId);
  if (el) el.style[prop] = value;
}

export function Focus(elId) {
  const el = _elements.get(elId);
  if (el && el.focus) el.focus();
}

export function SetProperty(elId, prop, value) {
  const el = _elements.get(elId);
  if (el) el[prop] = value;
}

export function GetProperty(elId, prop) {
  const el = _elements.get(elId);
  if (el) return String(el[prop] ?? '');
  return '';
}

export function AddClass(elId, cls) {
  const el = _elements.get(elId);
  if (el && el.classList) el.classList.add(cls);
}

export function RemoveClass(elId, cls) {
  const el = _elements.get(elId);
  if (el && el.classList) el.classList.remove(cls);
}

// Events.
export function AddEventListener(elId, event, callbackId) {
  const el = _elements.get(elId);
  const cb = _callbacks.get(callbackId);
  if (el && cb) el.addEventListener(event, cb);
}

// AddEnterKeyListener fires callback when Enter is pressed on the element.
export function AddEnterKeyListener(elId, callbackId) {
  const el = _elements.get(elId);
  const cb = _callbacks.get(callbackId);
  if (el && cb) el.addEventListener('keydown', function(e) {
    if (e.key === 'Enter') { e.preventDefault(); cb(); }
  });
}

// Like AddEventListener but only fires when e.target === e.currentTarget.
// Useful for backdrop click-to-close without catching child clicks.
export function AddSelfEventListener(elId, event, callbackId) {
  const el = _elements.get(elId);
  const cb = _callbacks.get(callbackId);
  if (el && cb) el.addEventListener(event, function(e) {
    if (e.target === e.currentTarget) cb();
  });
}

export function RemoveEventListener(elId, event, callbackId) {
  const el = _elements.get(elId);
  const cb = _callbacks.get(callbackId);
  if (el && cb) el.removeEventListener(event, cb);
}

// Register a Go function as a JS callback. Returns callback ID.
export function RegisterCallback(fn) {
  const id = _nextCb++;
  _callbacks.set(id, fn);
  return id;
}

export function ReleaseCallback(id) {
  _callbacks.delete(id);
}

// Scheduling.
export function RequestAnimationFrame(fn) {
  if (typeof window !== 'undefined') {
    window.requestAnimationFrame(fn);
  } else {
    setTimeout(fn, 16);
  }
}

export function SetTimeout(fn, ms) {
  return setTimeout(fn, ms);
}

export function SetInterval(fn, ms) {
  return setInterval(fn, ms);
}

export function ClearInterval(id) {
  clearInterval(id);
}

export function ClearTimeout(id) {
  clearTimeout(id);
}

// Cleanup: release element handle.
export function ReleaseElement(id) {
  _elements.delete(id);
}

// Fetch a URL as text, call fn with result.
export function FetchText(url, fn) {
  fetch(url).then(r => r.text()).then(t => { if (fn) fn(t); });
}

// Navigation.
export function NextSibling(elId) {
  const el = _elements.get(elId);
  if (el && el.nextSibling) {
    const id = _nextId++;
    _elements.set(id, el.nextSibling);
    return id;
  }
  return 0;
}

// Console / browser.
export function ConsoleLog(msg) {
  console.log(msg);
}

export function PrefersDark() {
  if (typeof window !== 'undefined' && window.matchMedia) {
    return window.matchMedia('(prefers-color-scheme: dark)').matches;
  }
  return false;
}

export function Confirm(msg) {
  if (typeof window !== 'undefined') return window.confirm(msg);
  return false;
}

// Service Worker communication.
let _swMessageHandler = null;

export function PostToSW(msg) {
  if (navigator.serviceWorker && navigator.serviceWorker.controller) {
    navigator.serviceWorker.controller.postMessage(msg);
  }
}

export function OnSWMessage(fn) {
  _swMessageHandler = fn;
  if (navigator.serviceWorker) {
    navigator.serviceWorker.addEventListener('message', function(e) {
      if (_swMessageHandler && typeof e.data === 'string') {
        _swMessageHandler(e.data);
      }
    });
  }
}

// IndexedDB (simple key-value).
let _idb = null;
const _idbName = 'smesh-kv';
const _idbStores = ['profiles', 'events', 'cache'];

function _openIDB() {
  return new Promise(function(resolve, reject) {
    if (_idb) { resolve(_idb); return; }
    const req = indexedDB.open(_idbName, 1);
    req.onupgradeneeded = function(e) {
      const db = e.target.result;
      for (const s of _idbStores) {
        if (!db.objectStoreNames.contains(s)) db.createObjectStore(s);
      }
    };
    req.onsuccess = function(e) { _idb = e.target.result; resolve(_idb); };
    req.onerror = function() { reject(req.error); };
  });
}

export function IDBGet(store, key, fn) {
  _openIDB().then(function(db) {
    const tx = db.transaction(store, 'readonly');
    const req = tx.objectStore(store).get(key);
    req.onsuccess = function() { fn(req.result ?? ''); };
    req.onerror = function() { fn(''); };
  }).catch(function() { fn(''); });
}

export function IDBPut(store, key, value) {
  _openIDB().then(function(db) {
    const tx = db.transaction(store, 'readwrite');
    tx.objectStore(store).put(value, key);
  });
}

export function IDBGetAll(store, fn, done) {
  _openIDB().then(function(db) {
    const tx = db.transaction(store, 'readonly');
    const req = tx.objectStore(store).openCursor();
    req.onsuccess = function(e) {
      const cursor = e.target.result;
      if (cursor) {
        fn(String(cursor.key), String(cursor.value));
        cursor.continue();
      } else {
        if (done) done();
      }
    };
    req.onerror = function() { if (done) done(); };
  }).catch(function() { if (done) done(); });
}

// Fetch relay NIP-11 info.
export function FetchRelayInfo(url, fn) {
  fetch(url, { headers: { 'Accept': 'application/nostr+json' } })
    .then(function(r) { return r.text(); })
    .then(function(t) { if (fn) fn(t); })
    .catch(function() { if (fn) fn(''); });
}

// History / location.
export function PushState(path) {
  if (typeof history !== 'undefined') history.pushState(null, '', path);
}

export function ReplaceState(path) {
  if (typeof history !== 'undefined') history.replaceState(null, '', path);
}

export function LocationReload() {
  if (typeof location !== 'undefined') location.reload();
}

export function GetPath() {
  if (typeof location !== 'undefined') return location.pathname;
  return '/';
}

export function Hostname() {
  if (typeof location !== 'undefined') return location.hostname;
  return '';
}

export function Port() {
  if (typeof location !== 'undefined') return location.port;
  return '';
}

export function UserAgent() {
  if (typeof navigator !== 'undefined') return navigator.userAgent;
  return '';
}

export function OnPopState(fn) {
  if (typeof window !== 'undefined') {
    window.addEventListener('popstate', function() {
      if (fn) fn(location.pathname);
    });
  }
}

// DownloadText triggers a file download with the given text content.
export function DownloadText(filename, content, mimeType) {
  const blob = new Blob([content], { type: mimeType || 'text/plain' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  setTimeout(function() { a.remove(); URL.revokeObjectURL(url); }, 1000);
}

// PickFileText opens a file picker and reads the selected file as text.
export function PickFileText(accept, fn) {
  const input = document.createElement('input');
  input.type = 'file';
  if (accept) input.accept = accept;
  input.style.display = 'none';
  input.addEventListener('change', function() {
    if (!input.files || !input.files[0]) { fn(''); return; }
    const reader = new FileReader();
    reader.onload = function() { fn(reader.result || ''); };
    reader.onerror = function() { fn(''); };
    reader.readAsText(input.files[0]);
  });
  document.body.appendChild(input);
  input.click();
  // Clean up after a delay (even if user cancels).
  setTimeout(function() { input.remove(); }, 60000);
}

// NowSeconds returns current Unix time in seconds.
export function NowSeconds() {
  return BigInt(Math.floor(Date.now() / 1000));
}

// Get raw element (for advanced use within JS runtime only).
export function getRawElement(id) {
  return _elements.get(id);
}
