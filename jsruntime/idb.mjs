// TinyJS Runtime — IndexedDB Bridge
// Provides Go-callable IndexedDB operations for event and DM storage.

const DB_NAME = 'smesh';
const DB_VERSION = 2;
let _db = null;
let _appVersion = '';

function _openDB(onReady) {
  const req = indexedDB.open(DB_NAME, DB_VERSION);
  req.onupgradeneeded = (e) => {
    const db = e.target.result;
    if (!db.objectStoreNames.contains('events')) {
      const ev = db.createObjectStore('events', { keyPath: 'id' });
      ev.createIndex('kind', 'kind', { unique: false });
      ev.createIndex('pubkey', 'pubkey', { unique: false });
      ev.createIndex('created_at', 'created_at', { unique: false });
      ev.createIndex('kind_created', ['kind', 'created_at'], { unique: false });
    }
    if (!db.objectStoreNames.contains('dms')) {
      const dm = db.createObjectStore('dms', { keyPath: 'id' });
      dm.createIndex('peer', 'peer', { unique: false });
      dm.createIndex('created_at', 'created_at', { unique: false });
      dm.createIndex('peer_created', ['peer', 'created_at'], { unique: false });
    }
    if (!db.objectStoreNames.contains('meta')) {
      db.createObjectStore('meta', { keyPath: 'key' });
    }
  };
  req.onsuccess = (e) => {
    _db = e.target.result;
    onReady();
  };
  req.onerror = (e) => {
    console.error('idb: open error:', e.target.error);
    onReady();
  };
}

function _checkVersion(fn) {
  if (!_db || !_appVersion) { fn(); return; }
  const tx = _db.transaction('meta', 'readonly');
  const store = tx.objectStore('meta');
  const req = store.get('version');
  req.onsuccess = () => {
    const stored = req.result ? req.result.value : '';
    if (stored && stored !== _appVersion) {
      // Version mismatch — flush all data stores.
      const clearTx = _db.transaction(['events', 'dms', 'meta'], 'readwrite');
      clearTx.objectStore('events').clear();
      clearTx.objectStore('dms').clear();
      clearTx.objectStore('meta').put({ key: 'version', value: _appVersion });
      clearTx.oncomplete = fn;
      clearTx.onerror = fn;
    } else if (!stored) {
      const writeTx = _db.transaction('meta', 'readwrite');
      writeTx.objectStore('meta').put({ key: 'version', value: _appVersion });
      writeTx.oncomplete = fn;
      writeTx.onerror = fn;
    } else {
      fn();
    }
  };
  req.onerror = fn;
}

export function SetVersion(v) {
  _appVersion = v;
}

export function Open(fn) {
  _openDB(() => {
    _checkVersion(() => { if (fn) fn(); });
  });
}

export function SaveEvent(eventJSON, fn) {
  if (!_db) { if (fn) fn(false); return; }
  let ev;
  try { ev = JSON.parse(eventJSON); } catch(e) { if (fn) fn(false); return; }
  const tx = _db.transaction('events', 'readwrite');
  const store = tx.objectStore('events');
  const check = store.get(ev.id);
  check.onsuccess = () => {
    if (check.result) {
      if (fn) fn(false); // duplicate
    } else {
      store.put(ev);
      tx.oncomplete = () => { if (fn) fn(true); };
      tx.onerror = () => { if (fn) fn(false); };
    }
  };
  check.onerror = () => { if (fn) fn(false); };
}

export function QueryEvents(filterJSON, fn) {
  if (!_db) { if (fn) fn('[]'); return; }
  let filter;
  try { filter = JSON.parse(filterJSON); } catch(e) { if (fn) fn('[]'); return; }
  const tx = _db.transaction('events', 'readonly');
  const store = tx.objectStore('events');
  const results = [];
  const limit = filter.limit || 500;

  // If filtering by specific IDs, fetch directly.
  if (filter.ids && filter.ids.length > 0) {
    let pending = filter.ids.length;
    for (const id of filter.ids) {
      const req = store.get(id);
      req.onsuccess = () => {
        if (req.result) results.push(req.result);
        if (--pending === 0) {
          if (fn) fn(JSON.stringify(results.slice(0, limit)));
        }
      };
      req.onerror = () => {
        if (--pending === 0) {
          if (fn) fn(JSON.stringify(results.slice(0, limit)));
        }
      };
    }
    return;
  }

  // Use kind+created_at index when filtering by kinds.
  let source;
  if (filter.kinds && filter.kinds.length === 1) {
    const kind = filter.kinds[0];
    const idx = store.index('kind_created');
    const lower = [kind, filter.since || 0];
    const upper = [kind, filter.until || Date.now() / 1000 + 86400];
    source = idx.openCursor(IDBKeyRange.bound(lower, upper), 'prev');
  } else {
    const idx = store.index('created_at');
    source = idx.openCursor(null, 'prev');
  }

  source.onsuccess = (e) => {
    const cursor = e.target.result;
    if (!cursor || results.length >= limit) {
      if (fn) fn(JSON.stringify(results));
      return;
    }
    const ev = cursor.value;
    if (_matchesFilter(ev, filter)) {
      results.push(ev);
    }
    cursor.continue();
  };
  source.onerror = () => { if (fn) fn('[]'); };
}

function _matchesFilter(ev, f) {
  if (f.kinds && f.kinds.length > 0 && !f.kinds.includes(ev.kind)) return false;
  if (f.authors && f.authors.length > 0 && !f.authors.includes(ev.pubkey)) return false;
  if (f.since && ev.created_at < f.since) return false;
  if (f.until && ev.created_at > f.until) return false;
  // Tag filters (#e, #p, etc.)
  for (const key of Object.keys(f)) {
    if (key.startsWith('#') && key.length === 2) {
      const tag = key[1];
      const vals = f[key];
      if (vals && vals.length > 0) {
        const evTags = (ev.tags || []).filter(t => t[0] === tag).map(t => t[1]);
        if (!vals.some(v => evTags.includes(v))) return false;
      }
    }
  }
  return true;
}

export function SaveDM(dmJSON, fn) {
  if (!_db) { if (fn) fn('error'); return; }
  let dm;
  try { dm = JSON.parse(dmJSON); } catch(e) { if (fn) fn('error'); return; }
  if (!dm.id) { if (fn) fn('error'); return; }
  const tx = _db.transaction('dms', 'readwrite');
  const store = tx.objectStore('dms');
  const check = store.get(dm.id);
  check.onsuccess = () => {
    if (check.result) {
      // Check if this is an upgrade (e.g. decrypted version replacing encrypted).
      if (dm.content && (!check.result.content || check.result.content !== dm.content)) {
        store.put(dm);
        tx.oncomplete = () => { if (fn) fn('upgraded'); };
        tx.onerror = () => { if (fn) fn('error'); };
      } else {
        if (fn) fn('duplicate');
      }
    } else {
      store.put(dm);
      tx.oncomplete = () => { if (fn) fn('saved'); };
      tx.onerror = () => { if (fn) fn('error'); };
    }
  };
  check.onerror = () => { if (fn) fn('error'); };
}

export function QueryDMs(peer, limit, until, fn) {
  if (!_db) { if (fn) fn('[]'); return; }
  const tx = _db.transaction('dms', 'readonly');
  const store = tx.objectStore('dms');
  const idx = store.index('peer_created');
  const results = [];
  const max = limit || 50;
  const upper = until > 0 ? until : Date.now() / 1000 + 86400;
  const range = IDBKeyRange.bound([peer, 0], [peer, upper]);
  const req = idx.openCursor(range, 'prev');
  req.onsuccess = (e) => {
    const cursor = e.target.result;
    if (!cursor || results.length >= max) {
      if (fn) fn(JSON.stringify(results));
      return;
    }
    results.push(cursor.value);
    cursor.continue();
  };
  req.onerror = () => { if (fn) fn('[]'); };
}

export function GetConversationList(fn) {
  if (!_db) { if (fn) fn('[]'); return; }
  const tx = _db.transaction('dms', 'readonly');
  const store = tx.objectStore('dms');
  const convos = new Map(); // peer -> latest DM
  const req = store.openCursor();
  req.onsuccess = (e) => {
    const cursor = e.target.result;
    if (!cursor) {
      const list = Array.from(convos.values());
      list.sort((a, b) => (b.created_at || 0) - (a.created_at || 0));
      if (fn) fn(JSON.stringify(list));
      return;
    }
    const dm = cursor.value;
    const peer = dm.peer || '';
    if (peer) {
      const existing = convos.get(peer);
      if (!existing || (dm.created_at || 0) > (existing.created_at || 0)) {
        convos.set(peer, dm);
      }
    }
    cursor.continue();
  };
  req.onerror = () => { if (fn) fn('[]'); };
}

export function ClearDMsByPeer(peer, fn) {
  if (!_db) { if (fn) fn(); return; }
  const tx = _db.transaction('dms', 'readwrite');
  const store = tx.objectStore('dms');
  const idx = store.index('peer');
  const req = idx.openCursor(IDBKeyRange.only(peer));
  req.onsuccess = (e) => {
    const cursor = e.target.result;
    if (!cursor) return;
    cursor.delete();
    cursor.continue();
  };
  tx.oncomplete = () => { if (fn) fn(); };
  tx.onerror = () => { if (fn) fn(); };
}
