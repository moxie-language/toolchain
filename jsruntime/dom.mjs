// TinyJS Runtime — DOM Bridge
// Provides Go-callable browser DOM operations.
// Elements are tracked by integer handles.
// Function names match Go signatures (PascalCase).

import { setKey as _setEncKey, sealStr, openStr, hasKey as _encHasKey } from './aead.mjs';

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

export function Remove(elId) {
  const el = _elements.get(elId);
  if (el && el.parentNode) el.parentNode.removeChild(el);
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

export function FirstElementChild(parentId) {
  const parent = _elements.get(parentId);
  if (parent && parent.firstElementChild) {
    const id = _nextId++;
    _elements.set(id, parent.firstElementChild);
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

// Contenteditable support — makes contenteditable divs behave like textareas
// for Moxie code that reads/writes "value", "selectionStart", "selectionEnd".

let _cePlaceholderCSS = false;

function _ceSetup(el) {
  if (!_cePlaceholderCSS) {
    const s = document.createElement('style');
    s.textContent = '[data-placeholder]:empty::before{content:attr(data-placeholder);color:var(--muted);pointer-events:none;}[data-placeholder] br:only-child{display:none;}';
    document.head.appendChild(s);
    _cePlaceholderCSS = true;
  }
  el.style.outline = 'none';
  const ph = el.getAttribute('placeholder');
  if (ph) { el.setAttribute('data-placeholder', ph); el.removeAttribute('placeholder'); }
  // Firefox bug 685799: Backspace/Delete can't remove contenteditable=false
  // inline elements. Detect deletion adjacent to a chip and remove it manually.
  el.addEventListener('beforeinput', function(e) {
    if (e.inputType !== 'deleteContentBackward' && e.inputType !== 'deleteContentForward') return;
    const sel = window.getSelection();
    if (!sel.rangeCount || !sel.isCollapsed) return;
    const r = sel.getRangeAt(0);
    let target = null;
    if (e.inputType === 'deleteContentBackward') {
      if (r.startContainer === el) {
        const prev = el.childNodes[r.startOffset - 1];
        if (prev && prev.dataset && prev.dataset.npub) target = prev;
      } else if (r.startContainer.nodeType === 3 && r.startOffset === 0) {
        const prev = r.startContainer.previousSibling;
        if (prev && prev.dataset && prev.dataset.npub) target = prev;
      }
    } else {
      if (r.startContainer === el) {
        const next = el.childNodes[r.startOffset];
        if (next && next.dataset && next.dataset.npub) target = next;
      } else if (r.startContainer.nodeType === 3 && r.startOffset === r.startContainer.textContent.length) {
        const next = r.startContainer.nextSibling;
        if (next && next.dataset && next.dataset.npub) target = next;
      }
    }
    if (target) {
      e.preventDefault();
      target.remove();
      el.dispatchEvent(new Event('input', { bubbles: true }));
    }
  });
}

function _isCE(el) { return el && (el.contentEditable === 'true' || el.contentEditable === 'plaintext-only'); }

function _ceExtract(el, stopNode, stopOff) {
  let text = '';
  let stopped = false;
  let resultOff = 0;
  function walk(node) {
    if (stopped) return;
    if (stopNode && node === stopNode) {
      if (node.nodeType === 3) {
        text += node.textContent.substring(0, stopOff);
      } else {
        let c = 0;
        for (const ch of node.childNodes) { if (c >= stopOff) break; walk(ch); c++; }
      }
      resultOff = text.length;
      stopped = true;
      return;
    }
    if (node.nodeType === 3) { text += node.textContent; }
    else if (node.nodeName === 'BR') { text += '\n'; }
    else if (node.dataset && node.dataset.npub) { text += 'nostr:' + node.dataset.npub; }
    else {
      if ((node.nodeName === 'DIV' || node.nodeName === 'P') && text.length > 0 && text[text.length - 1] !== '\n') text += '\n';
      for (const ch of node.childNodes) { if (stopped) return; walk(ch); }
    }
  }
  for (const ch of el.childNodes) { if (stopped) break; walk(ch); }
  if (!stopped) resultOff = text.length;
  return { text, offset: resultOff };
}

function _ceGetValue(el) { return _ceExtract(el).text; }

function _ceGetSelOff(el) {
  const sel = window.getSelection();
  if (!sel.rangeCount || !el.contains(sel.focusNode)) return 0;
  return _ceExtract(el, sel.focusNode, sel.focusOffset).offset;
}

function _ceSetSelOff(el, target) {
  let rem = target;
  let rNode = el, rOff = 0;
  let done = false;
  function walk(node) {
    if (done) return;
    if (node.nodeType === 3) {
      if (rem <= node.textContent.length) { rNode = node; rOff = rem; done = true; return; }
      rem -= node.textContent.length;
    } else if (node.nodeName === 'BR') {
      if (rem <= 1) { rNode = node.parentNode; rOff = Array.from(node.parentNode.childNodes).indexOf(node) + 1; done = true; return; }
      rem -= 1;
    } else if (node.dataset && node.dataset.npub) {
      const len = ('nostr:' + node.dataset.npub).length;
      if (rem <= len) { rNode = node.parentNode; rOff = Array.from(node.parentNode.childNodes).indexOf(node) + 1; done = true; return; }
      rem -= len;
    } else {
      if ((node.nodeName === 'DIV' || node.nodeName === 'P') && rem > 0) rem--;
      for (const ch of node.childNodes) { if (done) return; walk(ch); }
    }
  }
  for (const ch of el.childNodes) { if (done) break; walk(ch); }
  if (!done) { rNode = el; rOff = el.childNodes.length; }
  try {
    const sel = window.getSelection();
    const range = document.createRange();
    range.setStart(rNode, rOff);
    range.collapse(true);
    sel.removeAllRanges();
    sel.addRange(range);
  } catch(e) {}
}

function _ceSetValue(el, text) {
  while (el.firstChild) el.removeChild(el.firstChild);
  if (!text) return;
  const re = /(nostr:npub1[qpzry9x8gf2tvdw0s3jn54khce6mua7l]{58})/;
  const parts = text.split(re);
  for (const part of parts) {
    if (part.match(re)) {
      el.appendChild(_ceChip(part.slice(6), '', ''));
    } else {
      const lines = part.split('\n');
      for (let i = 0; i < lines.length; i++) {
        if (i > 0) el.appendChild(document.createElement('br'));
        if (lines[i]) el.appendChild(document.createTextNode(lines[i]));
      }
    }
  }
}

function _ceChip(npub, name, pic) {
  const s = document.createElement('span');
  s.contentEditable = 'false';
  s.dataset.npub = npub;
  s.style.cssText = 'display:inline-flex;align-items:center;gap:3px;background:rgba(100,100,255,0.15);border-radius:4px;padding:1px 5px;font-size:13px;vertical-align:baseline;cursor:default;user-select:all;line-height:1.4;';
  if (pic) {
    const img = document.createElement('img');
    img.src = pic;
    img.style.cssText = 'width:16px;height:16px;border-radius:50%;object-fit:cover;';
    s.appendChild(img);
  }
  s.appendChild(document.createTextNode(name || ('@' + npub.slice(0, 12) + '…')));
  return s;
}

export function InsertMentionChip(elId, charCount, npub, name, pic) {
  const el = _elements.get(elId);
  if (!el) return;
  if (_isCE(el)) {
    el.focus();
    const sel = window.getSelection();
    if (!sel.rangeCount) return;
    for (let i = 0; i < charCount; i++) sel.modify('extend', 'backward', 'character');
    const range = sel.getRangeAt(0);
    range.deleteContents();
    const chip = _ceChip(npub, name, pic);
    range.insertNode(chip);
    const space = document.createTextNode(' ');
    if (chip.nextSibling) chip.parentNode.insertBefore(space, chip.nextSibling);
    else chip.parentNode.appendChild(space);
    range.setStartAfter(space);
    range.collapse(true);
    sel.removeAllRanges();
    sel.addRange(range);
    el.dispatchEvent(new Event('input', { bubbles: true }));
  } else {
    const val = el.value || '';
    const cur = el.selectionStart || 0;
    const mention = 'nostr:' + npub + ' ';
    const start = cur - charCount;
    el.value = val.substring(0, start) + mention + val.substring(cur);
    const nc = start + mention.length;
    el.selectionStart = nc;
    el.selectionEnd = nc;
  }
}

// Properties and attributes.
export function SetAttribute(elId, name, value) {
  const el = _elements.get(elId);
  if (!el || !el.setAttribute) return;
  if (name === 'contenteditable' && (value === 'true' || value === 'plaintext-only')) {
    el.setAttribute(name, value);
    _ceSetup(el);
    return;
  }
  if (name === 'placeholder' && _isCE(el)) {
    el.setAttribute('data-placeholder', value);
    return;
  }
  el.setAttribute(name, value);
}

export function RemoveAttribute(elId, name) {
  const el = _elements.get(elId);
  if (el && el.removeAttribute) el.removeAttribute(name);
}

export function SetTextContent(elId, text) {
  const el = _elements.get(elId);
  if (el) el.textContent = text;
}

export function SetInnerHTML(elId, html) {
  const el = _elements.get(elId);
  if (!el) return;
  if (!html) {
    if (el.replaceChildren) el.replaceChildren();
    else while (el.firstChild) el.removeChild(el.firstChild);
    return;
  }
  const tpl = document.createElement('template');
  const prop = 'inner' + 'HTML';
  tpl[prop] = html;
  if (el.replaceChildren) el.replaceChildren(tpl.content);
  else {
    while (el.firstChild) el.removeChild(el.firstChild);
    el.appendChild(tpl.content);
  }
}

export function SetStyle(elId, prop, value) {
  const el = _elements.get(elId);
  if (el && el.style) el.style[prop] = value;
}

export function Focus(elId) {
  const el = _elements.get(elId);
  if (el && el.focus) el.focus();
}

export function SetProperty(elId, prop, value) {
  const el = _elements.get(elId);
  if (!el) return;
  if (_isCE(el)) {
    if (prop === 'value') { _ceSetValue(el, value); return; }
    if (prop === 'selectionStart' || prop === 'selectionEnd') { _ceSetSelOff(el, parseInt(value) || 0); return; }
  }
  el[prop] = value;
}

export function GetProperty(elId, prop) {
  const el = _elements.get(elId);
  if (!el) return '';
  if (_isCE(el)) {
    if (prop === 'value') return _ceGetValue(el);
    if (prop === 'selectionStart' || prop === 'selectionEnd') return String(_ceGetSelOff(el));
  }
  return String(el[prop] ?? '');
}

export function BoundingClientLeft(elId) {
  const el = _elements.get(elId);
  if (!el || !el.getBoundingClientRect) return 0;
  return el.getBoundingClientRect().left | 0;
}

export function GetBoundingRect(elId, fn) {
  const el = _elements.get(elId);
  if (!el || !el.getBoundingClientRect) { fn(0, 0, 0, 0, 0, 0); return; }
  const r = el.getBoundingClientRect();
  fn(Math.round(r.left), Math.round(r.top), Math.round(r.right), Math.round(r.bottom), Math.round(r.width), Math.round(r.height));
}

export function GetViewportHeight() {
  return window.innerHeight | 0;
}

export function GetViewportWidth() {
  return window.innerWidth | 0;
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

// OnKeydown registers a keydown handler that calls fn(key, prevent).
// fn receives the key name as a string. To preventDefault, fn should call
// the prevent callback.
export function OnKeydown(elId, fn) {
  const el = _elements.get(elId);
  if (!el) return;
  el.addEventListener('keydown', function(e) {
    let stopped = false;
    fn(e.key, function() { stopped = true; });
    if (stopped) { e.preventDefault(); e.stopPropagation(); }
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

// Service Worker communication — with direct WebSocket fallback for insecure contexts.
let _swMessageHandler = null;
let _swQueue = [];
const _hasSW = typeof navigator !== 'undefined' && 'serviceWorker' in navigator &&
  (typeof self !== 'undefined' && self.isSecureContext);

// ========================================================================
// Direct WebSocket relay fallback (active when SW unavailable)
// Handles PROXY, REQ, CLOSE, EVENT messages using NIP-01 WebSocket protocol.
// ========================================================================

let _wsConns = {};       // url -> WebSocket
let _wsSubs = {};        // subID -> { filter: string, relays: Set<url> }
let _wsSeen = {};        // subID -> { eventID: true }
let _wsEoseCount = {};   // subID -> count of relays that sent EOSE
let _wsWriteRelays = []; // relay URLs for EVENT publishing

function _diag(msg) {
  console.log(msg);
}
let _wsPendingPub = {};  // url -> [json strings] queued while CONNECTING

function _wsSend(handler, msg) {
  if (handler) handler(msg);
}

// Count an EOSE (or CLOSED) for a sub. Fire EOSE to the app on the FIRST
// relay response — matches the SW behaviour and prevents auth-requiring
// relays from blocking the threshold.
let _wsEoseFired = {};
function _wsCountEose(sid) {
  if (!_wsEoseCount[sid]) _wsEoseCount[sid] = 0;
  _wsEoseCount[sid]++;
  if (!_wsEoseFired[sid]) {
    _wsEoseFired[sid] = true;
    _wsSend(_swMessageHandler, '["EOSE",' + JSON.stringify(sid) + ']');
  }
}

function _wsEnsure(url) {
  if (_wsConns[url]) return _wsConns[url];
  var ws;
  try { ws = new WebSocket(url); } catch(e) { return null; }
  _wsConns[url] = ws;
  ws.onopen = function() {
    _diag('WS OPEN ' + url);
    // Replay active subs for this relay.
    for (var sid in _wsSubs) {
      var sub = _wsSubs[sid];
      if (sub.relays.has(url)) {
        ws.send(JSON.stringify(["REQ", sid, JSON.parse(sub.filter)]));
      }
    }
    // Flush queued publishes.
    var pending = _wsPendingPub[url];
    if (pending) {
      _diag('FLUSH ' + pending.length + ' queued to ' + url);
      delete _wsPendingPub[url];
      for (var p = 0; p < pending.length; p++) ws.send(pending[p]);
    }
  };
  ws.onmessage = function(e) {
    var msg;
    try { msg = JSON.parse(e.data); } catch(ex) { return; }
    if (!Array.isArray(msg)) return;
    if (msg[0] === "EVENT" && msg.length >= 3) {
      var sid = msg[1], ev = msg[2];
      if (!_wsSeen[sid]) _wsSeen[sid] = {};
      if (ev.id && _wsSeen[sid][ev.id]) return;
      if (ev.id) _wsSeen[sid][ev.id] = true;
      _wsSend(_swMessageHandler, '["EVENT",' + JSON.stringify(sid) + ',' + JSON.stringify(ev) + ']');
      if (ev.id) _wsSend(_swMessageHandler, '["SEEN_ON",' + JSON.stringify(ev.id) + ',' + JSON.stringify(url) + ']');
    } else if (msg[0] === "EOSE" && msg.length >= 2) {
      _wsCountEose(msg[1]);
    } else if (msg[0] === "CLOSED" && msg.length >= 2) {
      _diag('CLOSED ' + url + ' sub=' + msg[1] + ' reason=' + (msg[2] || ''));
      _wsCountEose(msg[1]);
    } else if (msg[0] === "OK" && msg.length >= 3) {
      var okId = msg[1], okSuccess = msg[2], okMsg = msg[3] || '';
      if (okSuccess) { _diag('OK ' + url + ' id=' + (okId && okId.substring(0,8))); }
      else { _diag('REJECTED ' + url + ' ' + (okId && okId.substring(0,8)) + ': ' + okMsg); }
    } else if (msg[0] === "NOTICE") {
      _diag('NOTICE ' + url + ': ' + msg[1]);
    }
  };
  ws.onclose = function() {
    _diag('WS CLOSE ' + url);
    delete _wsConns[url];
    // Trigger EOSE for subs that included this relay (connection died before EOSE).
    for (var sid in _wsSubs) {
      if (_wsSubs[sid].relays.has(url)) _wsCountEose(sid);
    }
  };
  ws.onerror = function() { ws.close(); };
  return ws;
}

function _wsCloseSub(sid) {
  var sub = _wsSubs[sid];
  if (!sub) return;
  sub.relays.forEach(function(url) {
    var ws = _wsConns[url];
    if (ws && ws.readyState === WebSocket.OPEN) {
      try { ws.send(JSON.stringify(["CLOSE", sid])); } catch(e) {}
    }
  });
  delete _wsSubs[sid];
  delete _wsSeen[sid];
  delete _wsEoseCount[sid];
  delete _wsEoseFired[sid];
}

function _wsHandleMsg(raw) {
  var arr;
  try { arr = JSON.parse(raw); } catch(e) { return; }
  if (!Array.isArray(arr)) return;
  var cmd = arr[0];
  if (cmd === "SET_PUBKEY") {
    // no-op for direct WS
  } else if (cmd === "SET_WRITE_RELAYS") {
    _wsWriteRelays = arr[1] || [];
    for (var i = 0; i < _wsWriteRelays.length; i++) _wsEnsure(_wsWriteRelays[i]);
  } else if (cmd === "PROXY") {
    var sid = arr[1], filter = arr[2], relays = arr[3] || [];
    _diag('PROXY ' + sid + ' relays=' + relays.length);
    _wsCloseSub(sid);
    _wsSubs[sid] = { filter: JSON.stringify(filter), relays: new Set(relays) };
    _wsSeen[sid] = {};
    _wsEoseCount[sid] = 0;
    for (var j = 0; j < relays.length; j++) {
      var url = relays[j];
      var ws = _wsConns[url];
      // Drop dead connections (CLOSING/CLOSED) so _wsEnsure creates a fresh one.
      if (ws && ws.readyState > 1) {
        _diag('PROXY ' + sid + ' drop dead ws ' + url + ' state=' + ws.readyState);
        delete _wsConns[url];
        ws = null;
      }
      ws = _wsEnsure(url);
      if (ws && ws.readyState === WebSocket.OPEN) {
        _diag('PROXY ' + sid + ' REQ ' + url);
        ws.send(JSON.stringify(["REQ", sid, filter]));
      } else {
        _diag('PROXY ' + sid + ' DEFER ' + url + ' state=' + (ws ? ws.readyState : 'null'));
      }
    }
  } else if (cmd === "REQ") {
    var sid2 = arr[1], filter2 = arr[2];
    var url = _wsWriteRelays[0];
    if (url) {
      _wsCloseSub(sid2);
      _wsSubs[sid2] = { filter: JSON.stringify(filter2), relays: new Set([url]) };
      _wsSeen[sid2] = {};
      _wsEoseCount[sid2] = 0;
      var ws2 = _wsEnsure(url);
      if (ws2 && ws2.readyState === WebSocket.OPEN) {
        ws2.send(JSON.stringify(["REQ", sid2, filter2]));
      }
    }
  } else if (cmd === "CLOSE") {
    _wsCloseSub(arr[1]);
  } else if (cmd === "PUBLISH_TO") {
    var ev2 = arr[1], relays2 = arr[2] || [];
    _diag('PUBLISH_TO kind=' + (ev2 && ev2.kind) + ' id=' + (ev2 && ev2.id && ev2.id.substring(0,8)) + ' relays=' + relays2.length);
    var data2 = JSON.stringify(["EVENT", ev2]);
    for (var m = 0; m < relays2.length; m++) {
      var ws4 = _wsEnsure(relays2[m]);
      if (ws4 && ws4.readyState === WebSocket.OPEN) { _diag('SEND ' + relays2[m]); ws4.send(data2); }
      else if (ws4) { _diag('QUEUE ' + relays2[m] + ' state=' + ws4.readyState); if (!_wsPendingPub[relays2[m]]) _wsPendingPub[relays2[m]] = []; _wsPendingPub[relays2[m]].push(data2); }
      else { _diag('FAIL no WS ' + relays2[m]); }
    }
  } else if (cmd === "EVENT") {
    _diag('EVENT kind=' + (arr[1] && arr[1].kind) + ' writeRelays=' + _wsWriteRelays.length);
    var ev = arr[1], data = JSON.stringify(["EVENT", ev]);
    for (var k = 0; k < _wsWriteRelays.length; k++) {
      var ws3 = _wsEnsure(_wsWriteRelays[k]);
      if (ws3 && ws3.readyState === WebSocket.OPEN) { _diag('SEND ' + _wsWriteRelays[k]); ws3.send(data); }
      else if (ws3) { var u3 = _wsWriteRelays[k]; _diag('QUEUE ' + u3 + ' state=' + ws3.readyState); if (!_wsPendingPub[u3]) _wsPendingPub[u3] = []; _wsPendingPub[u3].push(data); }
      else { _diag('FAIL no WS ' + _wsWriteRelays[k]); }
    }
  } else if (cmd === "CLEAR_KEY") {
    for (var sid3 in _wsSubs) _wsCloseSub(sid3);
    for (var u in _wsConns) { try { _wsConns[u].close(); } catch(e) {} }
    _wsConns = {};
    _wsWriteRelays = [];
    _wsPendingPub = {};
  }
}

// ========================================================================

// All PostToSW messages, in order. Used to replay to a new SW on controllerchange.
var _swSent = [];

export function PostToSW(msg) {
  if (!_hasSW) {
    _wsHandleMsg('' + msg);
    return;
  }
  var s = '' + msg;
  _swSent.push(s);
  if (navigator.serviceWorker.controller) {
    navigator.serviceWorker.controller.postMessage(s);
  } else {
    _swQueue.push(s);
  }
}

function _sendToSW() {
  if (!navigator.serviceWorker.controller) return;
  // Prefer queued messages (never sent). If queue is empty, replay all sent
  // messages (they went to an old SW that was replaced).
  var msgs;
  if (_swQueue.length > 0) {
    msgs = _swQueue;
    _swQueue = [];
  } else if (_swSent.length > 0) {
    msgs = _swSent.slice();
    _swSent = [];
  } else {
    return;
  }
  for (var i = 0; i < msgs.length; i++) {
    navigator.serviceWorker.controller.postMessage(msgs[i]);
  }
}

if (_hasSW) {
  navigator.serviceWorker.addEventListener('controllerchange', _sendToSW);
  navigator.serviceWorker.ready.then(function(reg) {
    if (!navigator.serviceWorker.controller && reg.active) {
      reg.active.postMessage('CLAIM');
    }
    setTimeout(_sendToSW, 100);
  });
}

export function OnSWMessage(fn) {
  _swMessageHandler = fn;
  if (_hasSW) {
    navigator.serviceWorker.addEventListener('message', function(e) {
      var d = e.data;
      if (typeof d !== 'string') {
        // Backward compat: old SW may send Slice objects via structured clone.
        if (d && d.$array && typeof d.$length === 'number') {
          var buf = new Uint8Array(d.$length);
          var off = d.$offset || 0;
          for (var i = 0; i < d.$length; i++) buf[i] = d.$array[off + i];
          d = new TextDecoder().decode(buf);
        } else {
          return; // Not a string and not a Slice — skip.
        }
      }
      if (_swMessageHandler) _swMessageHandler(d);
    });
  }
}

export function ReadClipboard(fn) {
  if (navigator.clipboard && navigator.clipboard.readText) {
    navigator.clipboard.readText().then(function(t) { fn(t || ''); }).catch(function() {
      var r = prompt('Paste nsec:');
      fn(r || '');
    });
  } else {
    var r = prompt('Paste nsec:');
    fn(r || '');
  }
}

export function WriteClipboard(text, fn) {
  const s = '' + text;
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(s).then(function() { fn(true); }).catch(function() { fn(false); });
    return;
  }
  try {
    const ta = document.createElement('textarea');
    ta.value = s;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.focus();
    ta.select();
    const ok = document.execCommand('copy');
    document.body.removeChild(ta);
    fn(!!ok);
  } catch (e) {
    fn(false);
  }
}

// IDB encryption key (set once per session).
export function IDBSetEncKey(hexKey) {
  const hex = '' + hexKey;
  const key = new Uint8Array(32);
  for (let i = 0; i < 32; i++) key[i] = parseInt(hex.substr(i * 2, 2), 16);
  _setEncKey(key);
}

// IndexedDB (simple key-value).
let _idb = null;
const _idbName = 'smesh-kv';
const _idbStores = ['profiles', 'events', 'cache', 'settings'];

// Coerce a value (JS string or moxiejs []byte Slice) to a native JS string.
// IDB requires native strings for store names and keys; Slice objects fail.
function _idbStr(v) {
  if (typeof v === 'string') return v;
  if (v && typeof v.$length === 'number' && v.$array) {
    const bytes = new Uint8Array(v.$length);
    for (let i = 0; i < v.$length; i++) bytes[i] = v.$array[v.$offset + i];
    return new TextDecoder().decode(bytes);
  }
  return String(v);
}

function _openIDB() {
  return new Promise(function(resolve, reject) {
    if (_idb) { resolve(_idb); return; }
    const req = indexedDB.open(_idbName, 2);
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
  const s = _idbStr(store), k = _idbStr(key);
  _openIDB().then(function(db) {
    const tx = db.transaction(s, 'readonly');
    const req = tx.objectStore(s).get(k);
    req.onsuccess = function() {
      const raw = req.result ?? '';
      fn(_encHasKey() ? openStr(raw) : raw);
    };
    req.onerror = function() { fn(''); };
  }).catch(function() { fn(''); });
}

export function IDBPut(store, key, value) {
  const s = _idbStr(store), k = _idbStr(key);
  _openIDB().then(function(db) {
    const tx = db.transaction(s, 'readwrite');
    const v = _encHasKey() ? sealStr('' + value) : value;
    tx.objectStore(s).put(v, k);
  });
}

export function IDBGetAll(store, fn, done) {
  const s = _idbStr(store);
  _openIDB().then(function(db) {
    const tx = db.transaction(s, 'readonly');
    const req = tx.objectStore(s).openCursor();
    req.onsuccess = function(e) {
      const cursor = e.target.result;
      if (cursor) {
        const val = _encHasKey() ? openStr(String(cursor.value)) : String(cursor.value);
        fn(String(cursor.key), val);
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

export function Back() {
  if (typeof history !== 'undefined') history.back();
}

export function LocationReload() {
  if (typeof location !== 'undefined') location.reload();
}

export function HardRefresh() {
  var p = [];
  if (typeof caches !== 'undefined') {
    p.push(caches.keys().then(function(keys) {
      return Promise.all(keys.map(function(k) { return caches.delete(k); }));
    }));
  }
  if (typeof navigator !== 'undefined' && navigator.serviceWorker) {
    p.push(navigator.serviceWorker.getRegistrations().then(function(regs) {
      regs.forEach(function(r) { r.unregister(); });
    }));
  }
  Promise.all(p).then(function() { location.reload(); });
}

export function ClearStoragePrefix(prefix) {
  if (typeof localStorage === 'undefined') return;
  var keys = [];
  for (var i = 0; i < localStorage.length; i++) {
    var k = localStorage.key(i);
    if (k && k.indexOf(prefix) === 0) keys.push(k);
  }
  for (var j = 0; j < keys.length; j++) localStorage.removeItem(keys[j]);
}

export function TimezoneOffsetSeconds() {
  // getTimezoneOffset() returns minutes, positive west of UTC.
  // We return seconds to ADD to UTC to get local time.
  return -(new Date().getTimezoneOffset()) * 60;
}

export function GetPath() {
  if (typeof location !== 'undefined') return location.pathname;
  return '/';
}

export function GetHash() {
  if (typeof location !== 'undefined') return location.hash;
  return '';
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

// OnPullRefresh registers a handler that fires when the user pulls to refresh.
// Handles both mouse wheel (accumulated up-scroll at top) and touch gestures.
export function OnPullRefresh(elId, indicatorId, fn) {
  const el = _elements.get(elId);
  const ind = _elements.get(indicatorId);
  if (!el) return;

  var pullAccum = 0;
  var pullTimer = 0;
  var threshold = 200;
  var resetMs = 400;

  el.addEventListener('wheel', function(e) {
    if (el.scrollTop > 0 || e.deltaY >= 0) {
      pullAccum = 0;
      if (ind) ind.style.display = 'none';
      return;
    }
    pullAccum += Math.abs(e.deltaY);
    clearTimeout(pullTimer);
    pullTimer = setTimeout(function() {
      pullAccum = 0;
      if (ind) ind.style.display = 'none';
    }, resetMs);
    if (ind && pullAccum > 40) ind.style.display = 'flex';
    if (pullAccum >= threshold) {
      pullAccum = 0;
      fn();
    }
  }, { passive: true });

  var touchStartY = 0;
  el.addEventListener('touchstart', function(e) {
    if (el.scrollTop === 0 && e.touches.length === 1) {
      touchStartY = e.touches[0].clientY;
    }
  }, { passive: true });
  el.addEventListener('touchmove', function(e) {
    if (el.scrollTop > 0 || touchStartY === 0) return;
    var dy = e.touches[0].clientY - touchStartY;
    if (dy > 20 && ind) ind.style.display = 'flex';
    if (dy > 80) {
      touchStartY = 0;
      fn();
    }
  }, { passive: true });
  el.addEventListener('touchend', function() {
    touchStartY = 0;
    if (pullAccum < threshold && ind) ind.style.display = 'none';
  }, { passive: true });
}

// PickFileBase64: open file picker, read as base64, call fn(base64, mime).
export function PickFileBase64(accept, fn) {
  const input = document.createElement('input');
  input.type = 'file';
  if (accept) input.accept = accept;
  input.style.display = 'none';
  input.addEventListener('change', function() {
    if (!input.files || !input.files[0]) { fn('', ''); return; }
    const file = input.files[0];
    const mime = file.type || 'application/octet-stream';
    const reader = new FileReader();
    reader.onload = function() {
      const b64 = reader.result.split(',')[1] || '';
      fn(b64, mime);
    };
    reader.onerror = function() { fn('', ''); };
    reader.readAsDataURL(file);
    input.remove();
  });
  document.body.appendChild(input);
  input.click();
  setTimeout(function() { input.remove(); }, 120000);
}

// OnPasteImage: listen for paste events with image data on an element.
export function OnPasteImage(elId, fn) {
  const el = _elements.get(elId);
  if (!el) return;
  el.addEventListener('paste', function(e) {
    const items = e.clipboardData && e.clipboardData.items;
    if (!items) return;
    for (let i = 0; i < items.length; i++) {
      if (items[i].type.indexOf('image/') === 0) {
        e.preventDefault();
        const mime = items[i].type;
        const blob = items[i].getAsFile();
        if (!blob) continue;
        const reader = new FileReader();
        reader.onload = function() {
          const b64 = reader.result.split(',')[1] || '';
          fn(b64, mime);
        };
        reader.readAsDataURL(blob);
        return;
      }
    }
  });
}

// OnDropImage: listen for drop events with image files on an element.
export function OnDropImage(elId, fn) {
  const el = _elements.get(elId);
  if (!el) return;
  el.addEventListener('dragenter', function(e) { e.preventDefault(); e.stopPropagation(); });
  el.addEventListener('dragover', function(e) { e.preventDefault(); e.stopPropagation(); });
  el.addEventListener('drop', function(e) {
    e.preventDefault();
    e.stopPropagation();
    const files = e.dataTransfer && e.dataTransfer.files;
    if (!files || files.length === 0) return;
    for (let i = 0; i < files.length; i++) {
      const f = files[i];
      const mime = f.type || 'application/octet-stream';
      if (mime.indexOf('image/') !== 0) continue;
      const reader = new FileReader();
      reader.onload = function() {
        const b64 = reader.result.split(',')[1] || '';
        fn(b64, mime);
      };
      reader.readAsDataURL(f);
    }
  });
}

// FetchPutBlobBase64: PUT binary data (from base64) to a URL with auth header.
export function FetchPutBlobBase64(url, base64data, contentType, authHeader, fn) {
  try {
    const raw = atob(base64data);
    const arr = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) arr[i] = raw.charCodeAt(i);
    const headers = { 'Content-Type': contentType || 'application/octet-stream' };
    if (authHeader) headers['Authorization'] = authHeader;
    fetch(url, { method: 'PUT', headers: headers, body: arr })
      .then(function(r) { return r.text(); })
      .then(function(t) { if (fn) fn(t); })
      .catch(function() { if (fn) fn(''); });
  } catch(e) {
    if (fn) fn('');
  }
}

// Get raw element (for advanced use within JS runtime only).
export function getRawElement(id) {
  return _elements.get(id);
}
