// WASM Loader — instantiates a Moxie WASM binary with all bridge imports.
// Reuses existing jsruntime modules for browser API surface.

import * as dom from './dom.mjs';
import * as webgl from './webgl.mjs';
import * as app from './app.mjs';
import * as text from './text.mjs';
import * as localstorage from './localstorage.mjs';
import * as ws from './ws.mjs';
import * as schnorr from './schnorr.mjs';
import * as ed25519 from './ed25519.mjs';
import * as x25519 from './x25519.mjs';
import * as p256 from './p256.mjs';
import * as poly1305 from './poly1305.mjs';
import * as crypto from './crypto.mjs';
import * as subtle from './subtle.mjs';
import * as signer from './signer.mjs';
import * as ext from './ext.mjs';
import * as idb from './idb.mjs';
import * as sw from './sw.mjs';
import * as markdown from './markdown.mjs';
import * as node from './node.mjs';
import * as sabChannel from './sab-channel.mjs';

let memory;
let instance;
const encoder = new TextEncoder();
const decoder = new TextDecoder();

function readStr(ptr, len) {
  if (len <= 0) return '';
  return decoder.decode(new Uint8Array(memory.buffer, ptr, len));
}

function readBytes(ptr, len) {
  if (len <= 0) return new Uint8Array(0);
  return new Uint8Array(memory.buffer, ptr, len).slice();
}

function writeStr(s) {
  const buf = encoder.encode(s);
  const ptr = instance.exports.__alloc(buf.length);
  new Uint8Array(memory.buffer, ptr, buf.length).set(buf);
  return { ptr, len: buf.length };
}

function writeBytes(b) {
  if (!b || b.length === 0) return { ptr: 0, len: 0 };
  const ptr = instance.exports.__alloc(b.length);
  new Uint8Array(memory.buffer, ptr, b.length).set(b instanceof Uint8Array ? b : new Uint8Array(b));
  return { ptr, len: b.length };
}

function setOutStr(outPtr, outLen, s) {
  const { ptr, len } = writeStr(s);
  const dv = new DataView(memory.buffer);
  dv.setInt32(outPtr, ptr, true);
  dv.setInt32(outLen, len, true);
}

function setOutBytes(outPtr, outLen, b) {
  const { ptr, len } = writeBytes(b);
  const dv = new DataView(memory.buffer);
  dv.setInt32(outPtr, ptr, true);
  dv.setInt32(outLen, len, true);
}

function setOutI32(addr, val) {
  new DataView(memory.buffer).setInt32(addr, val, true);
}

// Callback dispatch — always deferred via queueMicrotask.
function cb0(id) { queueMicrotask(() => instance.exports.__cb0(id)); }
function cbs(id, s) {
  queueMicrotask(() => {
    const { ptr, len } = writeStr(s);
    instance.exports.__cbs(id, ptr, len);
  });
}
function cbb(id, v) { queueMicrotask(() => instance.exports.__cbb(id, v ? 1 : 0)); }
function cbi(id, v) { queueMicrotask(() => instance.exports.__cbi(id, v | 0)); }
function cbis(id, i, s) {
  queueMicrotask(() => {
    const { ptr, len } = writeStr(s);
    instance.exports.__cbis(id, i | 0, ptr, len);
  });
}
function cbiis(id, a, b, s) {
  queueMicrotask(() => {
    const { ptr, len } = writeStr(s);
    instance.exports.__cbiis(id, a | 0, b | 0, ptr, len);
  });
}
function cbss(id, s1, s2) {
  queueMicrotask(() => {
    const a = writeStr(s1);
    const b = writeStr(s2);
    instance.exports.__cbss(id, a.ptr, a.len, b.ptr, b.len);
  });
}
function cb6i(id, a, b, c, d, e, f) {
  queueMicrotask(() => instance.exports.__cb6i(id, a|0, b|0, c|0, d|0, e|0, f|0));
}
function cbii(id, a, b) { queueMicrotask(() => instance.exports.__cbii(id, a|0, b|0)); }
function cbiii(id, a, b, c) { queueMicrotask(() => instance.exports.__cbiii(id, a|0, b|0, c|0)); }
function cbdata(id, bytes) {
  queueMicrotask(() => {
    const { ptr, len } = writeBytes(bytes);
    instance.exports.__cbdata(id, ptr, len);
  });
}

// Parse null-separated string list.
function splitNull(ptr, len, count) {
  if (count <= 0) return [];
  const s = readStr(ptr, len);
  return s.split('\0');
}

// Track JS wrappers for WASM event listeners so RemoveEventListener works.
const jsListeners = new Map();
function listenerKey(elId, event, cbID) { return `${elId}:${event}:${cbID}`; }

// Hook response tracking for nested callbacks.
const hookResponses = new Map();
let hookNextReq = 1;

// buildBridge returns the import object for the "bridge" module. The same
// shape is used by the page and by a spawned domain's Worker; the two differ
// only in which end of the channel rings they own, so the channel table and
// the worker flag are parameters.
//
// opts.channels: Map<int32, SharedArrayBuffer> of rings bound at spawn. On
//   the page side this starts empty and channel_create fills it; in a worker
//   the handles arrive from the parent and this table is pre-populated so the
//   child reads the far end of the same rings.
// opts.isWorker: 1 inside a spawned domain, 0 in the page.
export function buildBridge(opts = {}) {
  const handles = opts.channels || new Map();
  let next = 1;
  const isWorker = opts.isWorker ? 1 : 0;
  return {
    // --- localstorage ---
    localstorage_get_item(kp, kl, _kc, op, ol) {
      setOutStr(op, ol, localstorage.GetItem(readStr(kp, kl)));
    },
    localstorage_set_item(kp, kl, _kc, vp, vl, _vc) {
      localstorage.SetItem(readStr(kp, kl), readStr(vp, vl));
    },
    localstorage_remove_item(kp, kl, _kc) {
      localstorage.RemoveItem(readStr(kp, kl));
    },

    // --- crypto ---
    crypto_pubkey_from_seckey(p, l, _c, op, ol) {
      setOutBytes(op, ol, crypto.PubKeyFromSecKey(readBytes(p, l)));
    },
    crypto_sign_schnorr(skp, skl, _skc, mp, ml, _mc, ap, al, _ac, op, ol) {
      setOutBytes(op, ol, crypto.SignSchnorr(readBytes(skp, skl), readBytes(mp, ml), readBytes(ap, al)));
    },
    crypto_verify_schnorr(pkp, pkl, _pkc, mp, ml, _mc, sp, sl, _sc) {
      return crypto.VerifySchnorr(readBytes(pkp, pkl), readBytes(mp, ml), readBytes(sp, sl)) ? 1 : 0;
    },

    // --- schnorr ---
    schnorr_pubkey_from_seckey(p, l, _c, op, ol, ok) {
      try {
        const r = schnorr.PubKeyFromSecKey(readBytes(p, l));
        setOutBytes(op, ol, r); setOutI32(ok, r ? 1 : 0);
      } catch { setOutI32(ok, 0); }
    },
    schnorr_sign(skp, skl, _skc, mp, ml, _mc, ap, al, _ac, op, ol, ok) {
      try {
        const r = schnorr.SignSchnorr(readBytes(skp, skl), readBytes(mp, ml), readBytes(ap, al));
        setOutBytes(op, ol, r); setOutI32(ok, r ? 1 : 0);
      } catch { setOutI32(ok, 0); }
    },
    schnorr_verify(pkp, pkl, _pkc, mp, ml, _mc, sp, sl, _sc) {
      return schnorr.VerifySchnorr(readBytes(pkp, pkl), readBytes(mp, ml), readBytes(sp, sl)) ? 1 : 0;
    },
    schnorr_ecdh(skp, skl, _skc, pkp, pkl, _pkc, op, ol, ok) {
      try {
        const r = schnorr.ECDH(readBytes(skp, skl), readBytes(pkp, pkl));
        setOutBytes(op, ol, r); setOutI32(ok, r ? 1 : 0);
      } catch { setOutI32(ok, 0); }
    },
    schnorr_sha256sum(p, l, _c, op, ol) {
      setOutBytes(op, ol, schnorr.SHA256Sum(readBytes(p, l)));
    },
    schnorr_scalar_add_mod_n(ap, al, _ac, bp, bl, _bc, op, ol, ok) {
      try {
        const r = schnorr.ScalarAddModN(readBytes(ap, al), readBytes(bp, bl));
        setOutBytes(op, ol, r); setOutI32(ok, r ? 1 : 0);
      } catch { setOutI32(ok, 0); }
    },
    schnorr_compressed_pubkey(p, l, _c, op, ol, ok) {
      try {
        const r = schnorr.CompressedPubKey(readBytes(p, l));
        setOutBytes(op, ol, r); setOutI32(ok, r ? 1 : 0);
      } catch { setOutI32(ok, 0); }
    },

    // --- ed25519 ---
    ed25519_new_key_from_seed(p, l, _c, op, ol) {
      setOutBytes(op, ol, ed25519.NewKeyFromSeed(readBytes(p, l)));
    },
    ed25519_sign(sp, sl, _sc, mp, ml, _mc, op, ol) {
      setOutBytes(op, ol, ed25519.Sign(readBytes(sp, sl), readBytes(mp, ml)));
    },
    ed25519_verify(pkp, pkl, _pkc, mp, ml, _mc, sp, sl, _sc) {
      return ed25519.Verify(readBytes(pkp, pkl), readBytes(mp, ml), readBytes(sp, sl)) ? 1 : 0;
    },

    // --- x25519 ---
    x25519_scalar_mult(sp, sl, _sc, pp, pl, _pc, op, ol) {
      setOutBytes(op, ol, x25519.ScalarMult(readBytes(sp, sl), readBytes(pp, pl)));
    },
    x25519_scalar_base_mult(sp, sl, _sc, op, ol) {
      setOutBytes(op, ol, x25519.ScalarBaseMult(readBytes(sp, sl)));
    },

    // --- p256 ---
    p256_scalar_base_mult(p, l, _c, op, ol) {
      setOutBytes(op, ol, p256.ScalarBaseMult(readBytes(p, l)));
    },
    p256_scalar_mult(sp, sl, _sc, pp, pl, _pc, op, ol) {
      setOutBytes(op, ol, p256.ScalarMult(readBytes(sp, sl), readBytes(pp, pl)));
    },
    p256_sign(sp, sl, _sc, mp, ml, _mc, op, ol) {
      setOutBytes(op, ol, p256.Sign(readBytes(sp, sl), readBytes(mp, ml)));
    },
    p256_verify(pkp, pkl, _pkc, mp, ml, _mc, sp, sl, _sc) {
      return p256.Verify(readBytes(pkp, pkl), readBytes(mp, ml), readBytes(sp, sl)) ? 1 : 0;
    },
    p256_reduce_scalar(p, l, _c, op, ol) {
      setOutBytes(op, ol, p256.ReduceScalar(readBytes(p, l)));
    },
    p256_valid_scalar(p, l, _c) {
      return p256.ValidScalar(readBytes(p, l)) ? 1 : 0;
    },

    // --- poly1305 ---
    poly1305_mac(kp, kl, _kc, dp, dl, _dc, op, ol) {
      setOutBytes(op, ol, poly1305.MAC(readBytes(kp, kl), readBytes(dp, dl)));
    },
    poly1305_verify(kp, kl, _kc, dp, dl, _dc, tp, tl, _tc) {
      return poly1305.Verify(readBytes(kp, kl), readBytes(dp, dl), readBytes(tp, tl)) ? 1 : 0;
    },

    // --- markdown ---
    markdown_render(p, l, _c, op, ol) {
      setOutStr(op, ol, markdown.Render(readStr(p, l)));
    },

    // --- dom ---
    dom_body() { return dom.Body(); },
    dom_create_element(p, l, _c) { return dom.CreateElement(readStr(p, l)); },
    dom_create_text_node(p, l, _c) { return dom.CreateTextNode(readStr(p, l)); },
    dom_get_element_by_id(p, l, _c) { return dom.GetElementById(readStr(p, l)); },
    dom_query_selector(p, l, _c) { return dom.QuerySelector(readStr(p, l)); },
    dom_query_selector_from(root, p, l, _c) { return dom.QuerySelectorFrom(root, readStr(p, l)); },
    dom_append_child(parent, child) { dom.AppendChild(parent, child); },
    dom_remove_child(parent, child) { dom.RemoveChild(parent, child); },
    dom_remove(el) { dom.Remove(el); },
    dom_insert_before(parent, nc, rc) { dom.InsertBefore(parent, nc, rc); },
    dom_first_child(el) { return dom.FirstChild(el); },
    dom_first_element_child(el) { return dom.FirstElementChild(el); },
    dom_next_sibling(el) { return dom.NextSibling(el); },
    dom_release_element(el) { dom.ReleaseElement(el); },
    dom_set_attribute(el, np, nl, _nc, vp, vl, _vc) {
      dom.SetAttribute(el, readStr(np, nl), readStr(vp, vl));
    },
    dom_remove_attribute(el, np, nl, _nc) { dom.RemoveAttribute(el, readStr(np, nl)); },
    dom_get_attribute(el, np, nl, _nc, op, ol) { setOutStr(op, ol, dom.GetAttribute(el, readStr(np, nl))); },
    dom_set_text_content(el, p, l, _c) { dom.SetTextContent(el, readStr(p, l)); },
    dom_set_inner_html(el, p, l, _c) { dom.SetInnerHTML(el, readStr(p, l)); },
    dom_set_style(el, pp, pl, _pc, vp, vl, _vc) {
      dom.SetStyle(el, readStr(pp, pl), readStr(vp, vl));
    },
    dom_add_class(el, p, l, _c) { dom.AddClass(el, readStr(p, l)); },
    dom_remove_class(el, p, l, _c) { dom.RemoveClass(el, readStr(p, l)); },
    dom_focus(el) { dom.Focus(el); },
    dom_set_property(el, pp, pl, _pc, vp, vl, _vc) {
      dom.SetProperty(el, readStr(pp, pl), readStr(vp, vl));
    },
    dom_get_property(el, pp, pl, _pc, op, ol) {
      setOutStr(op, ol, dom.GetProperty(el, readStr(pp, pl)));
    },
    dom_bounding_client_left(el) { return dom.BoundingClientLeft(el); },
    dom_get_bounding_rect(el, cbID) {
      const r = dom.GetBoundingRect_sync ? dom.GetBoundingRect_sync(el) : null;
      if (r) { cb6i(cbID, r.left|0, r.top|0, r.right|0, r.bottom|0, r.width|0, r.height|0); }
      else { dom.GetBoundingRect(el, (l,t,r,b,w,h) => cb6i(cbID, l,t,r,b,w,h)); }
    },
    dom_get_viewport_height() { return dom.GetViewportHeight(); },
    dom_get_viewport_width() { return dom.GetViewportWidth(); },
    dom_add_event_listener(elId, ep, el2, _ec, cbID) {
      const el = dom.getElement(elId);
      if (!el) return;
      const event = readStr(ep, el2);
      const fn = () => cb0(cbID);
      el.addEventListener(event, fn);
      jsListeners.set(listenerKey(elId, event, cbID), fn);
    },
    dom_add_self_event_listener(elId, ep, el2, _ec, cbID) {
      const el = dom.getElement(elId);
      if (!el) return;
      const event = readStr(ep, el2);
      const fn = (e) => { if (e.target === e.currentTarget) cb0(cbID); };
      el.addEventListener(event, fn);
      jsListeners.set(listenerKey(elId, event, cbID), fn);
    },
    dom_add_enter_key_listener(elId, cbID) {
      const el = dom.getElement(elId);
      if (!el) return;
      const fn = (e) => { if (e.key === 'Enter') { e.preventDefault(); cb0(cbID); } };
      el.addEventListener('keydown', fn);
      jsListeners.set(listenerKey(elId, 'keydown:enter', cbID), fn);
    },
    dom_remove_event_listener(elId, ep, el2, _ec, cbID) {
      const el = dom.getElement(elId);
      if (!el) return;
      const event = readStr(ep, el2);
      const key = listenerKey(elId, event, cbID);
      const fn = jsListeners.get(key);
      if (fn) { el.removeEventListener(event, fn); jsListeners.delete(key); }
    },
    dom_request_animation_frame(cbID) { requestAnimationFrame(() => cb0(cbID)); },
    dom_observe_resize(elId, cbID) { dom.ObserveResize(elId, cbID); },
    dom_unobserve_resize(elId) { dom.UnobserveResize(elId); },
    dom_on_keydown(el, handlerID) {
      const element = dom.getElement(el);
      if (!element) return;
      element.addEventListener('keydown', (e) => {
        const { ptr, len } = writeStr(e.key);
        const prevented = instance.exports.__hook_keydown(handlerID, ptr, len);
        if (prevented) e.preventDefault();
      });
    },
    dom_insert_mention_chip(el, charCount, np, nl, _nc, nmp, nml, _nmc, pp, pl, _pc) {
      dom.InsertMentionChip(el, charCount, readStr(np, nl), readStr(nmp, nml), readStr(pp, pl));
    },
    dom_set_timeout(cbID, ms) {
      return setTimeout(() => cb0(cbID), ms);
    },
    dom_clear_timeout(id) { clearTimeout(id); },
    dom_fetch_text(p, l, _c, cbID) {
      dom.FetchText(readStr(p, l), (s) => cbs(cbID, s));
    },
    dom_fetch_relay_info(p, l, _c, cbID) {
      dom.FetchRelayInfo(readStr(p, l), (s) => cbs(cbID, s));
    },
    dom_fetch_put_blob_base64(up, ul, _uc, dp, dl, _dc, cp, cl, _cc, ap, al, _ac, cbID) {
      dom.FetchPutBlobBase64(readStr(up, ul), readStr(dp, dl), readStr(cp, cl), readStr(ap, al), (s) => cbs(cbID, s));
    },
    dom_idb_set_enc_key(p, l, _c) { dom.IDBSetEncKey(readStr(p, l)); },
    dom_idb_get(sp, sl, _sc, kp, kl, _kc, cbID) {
      dom.IDBGet(readStr(sp, sl), readStr(kp, kl), (s) => cbs(cbID, s));
    },
    dom_idb_put(sp, sl, _sc, kp, kl, _kc, vp, vl, _vc) {
      dom.IDBPut(readStr(sp, sl), readStr(kp, kl), readStr(vp, vl));
    },
    dom_idb_get_all(sp, sl, _sc, fnID, doneID) {
      dom.IDBGetAll(readStr(sp, sl), (k, v) => cbss(fnID, k, v), () => cb0(doneID));
    },
    dom_prefers_dark() { return dom.PrefersDark() ? 1 : 0; },
    dom_console_log(p, l, _c) { console.log(readStr(p, l)); },
    dom_confirm(p, l, _c) { return confirm(readStr(p, l)) ? 1 : 0; },
    dom_post_to_sw(p, l, _c) { dom.PostToSW(readStr(p, l)); },
    dom_on_sw_message(cbID) { dom.OnSWMessage((s) => cbs(cbID, s)); },
    dom_push_state(p, l, _c) { dom.PushState(readStr(p, l)); },
    dom_replace_state(p, l, _c) { dom.ReplaceState(readStr(p, l)); },
    dom_back() { history.back(); },
    dom_location_reload() { location.reload(); },
    dom_hard_refresh() { dom.HardRefresh(); },
    dom_clear_storage_prefix(p, l, _c) { dom.ClearStoragePrefix(readStr(p, l)); },
    dom_timezone_offset_seconds() { return -(new Date().getTimezoneOffset()) * 60; },
    dom_read_clipboard(cbID) { dom.ReadClipboard((s) => cbs(cbID, s)); },
    dom_write_clipboard(p, l, _c, cbID) { dom.WriteClipboard(readStr(p, l), (ok) => cbb(cbID, ok)); },
    dom_get_path(op, ol) { setOutStr(op, ol, location.pathname); },
    dom_get_hash(op, ol) { setOutStr(op, ol, location.hash); },
    dom_hostname(op, ol) { setOutStr(op, ol, location.hostname); },
    dom_port(op, ol) { setOutStr(op, ol, location.port); },
    dom_user_agent(op, ol) { setOutStr(op, ol, navigator.userAgent); },
    dom_on_pop_state(cbID) {
      window.addEventListener('popstate', () => cbs(cbID, location.pathname));
    },
    dom_pick_file_text(p, l, _c, cbID) { dom.PickFileText(readStr(p, l), (s) => cbs(cbID, s)); },
    dom_download_text(fp, fl, _fc, cp, cl, _cc, mp, ml, _mc) {
      dom.DownloadText(readStr(fp, fl), readStr(cp, cl), readStr(mp, ml));
    },
    dom_on_pull_refresh(el, ind, cbID) { dom.OnPullRefresh(el, ind, () => cb0(cbID)); },
    dom_now_seconds() { return BigInt(Math.floor(Date.now() / 1000)); },
    dom_pick_file_base64(p, l, _c, cbID) {
      dom.PickFileBase64(readStr(p, l), (d, m) => cbss(cbID, d, m));
    },
    dom_on_paste_image(el, cbID) { dom.OnPasteImage(el, (d, m) => cbss(cbID, d, m)); },
    dom_on_drop_image(el, cbID) { dom.OnDropImage(el, (d, m) => cbss(cbID, d, m)); },

    // --- ws ---
    ws_dial(up, ul, _uc, msgID, openID, closeID, errID) {
      return ws.Dial(readStr(up, ul),
        (conn, s) => cbis(msgID, conn, s),
        (conn) => cbi(openID, conn),
        (conn, code, reason) => cbiis(closeID, conn, code, reason),
        (conn) => cbi(errID, conn),
      );
    },
    ws_send(conn, mp, ml, _mc) { return ws.Send(conn, readStr(mp, ml)) ? 1 : 0; },
    ws_close(conn) { ws.Close(conn); },
    ws_ready_state(conn) { return ws.ReadyState(conn); },

    // --- bc ---
    bc_open(np, nl, _nc, cbID) {
      return (typeof BroadcastChannel !== 'undefined')
        ? (() => { const bc = new BroadcastChannel(readStr(np, nl)); bc.onmessage = (e) => cbs(cbID, String(e.data)); return 1; })()
        : 0;
    },
    bc_send(handle, mp, ml, _mc) { /* placeholder - needs handle tracking */ },
    bc_close(handle) { /* placeholder */ },

    // --- wasm ---
    wasm_load_go_wasm(ep, el, _ec, wp, wl, _wc, cbID) {
      // Placeholder: loads another Go WASM binary
      cb0(cbID);
    },

    // --- node ---
    node_on_line(cbID) { if (node.OnLine) node.OnLine((s) => cbs(cbID, s)); },
    node_on_close(cbID) { if (node.OnClose) node.OnClose(() => cb0(cbID)); },
    node_write_line(p, l, _c) { if (node.WriteLine) node.WriteLine(readStr(p, l)); },
    node_write_err(p, l, _c) { if (node.WriteErr) node.WriteErr(readStr(p, l)); },
    node_exit(code) { if (node.Exit) node.Exit(code); },
    node_now_seconds() { return BigInt(Math.floor(Date.now() / 1000)); },

    // --- subtle ---
    subtle_random_bytes(p, l, _c) {
      const buf = new Uint8Array(memory.buffer, p, l);
      globalThis.crypto.getRandomValues(buf);
    },
    subtle_aes_cbc_encrypt(kp, kl, _kc, ip, il, _ic, pp, pl, _pc, cbID) {
      subtle.AESCBCEncrypt(readBytes(kp, kl), readBytes(ip, il), readBytes(pp, pl), (r) => cbdata(cbID, r));
    },
    subtle_aes_cbc_decrypt(kp, kl, _kc, ip, il, _ic, cp, cl, _cc, cbID) {
      subtle.AESCBCDecrypt(readBytes(kp, kl), readBytes(ip, il), readBytes(cp, cl), (r) => cbdata(cbID, r));
    },
    subtle_aes_gcm_encrypt(kp, kl, _kc, ip, il, _ic, pp, pl, _pc, cbID) {
      subtle.AESGCMEncrypt(readBytes(kp, kl), readBytes(ip, il), readBytes(pp, pl), (r) => cbdata(cbID, r));
    },
    subtle_aes_gcm_decrypt(kp, kl, _kc, ip, il, _ic, cp, cl, _cc, cbID) {
      subtle.AESGCMDecrypt(readBytes(kp, kl), readBytes(ip, il), readBytes(cp, cl), (r) => cbdata(cbID, r));
    },
    subtle_pbkdf2_derive_key(pp, pl, _pc, sp, sl, _sc, iterations, cbID) {
      subtle.PBKDF2DeriveKey(readStr(pp, pl), readBytes(sp, sl), iterations, (r) => cbdata(cbID, r));
    },
    subtle_argon2id_derive_key(pp, pl, _pc, sp, sl, _sc, t, m, p, dkLen, cbID) {
      subtle.Argon2idDeriveKey(readStr(pp, pl), readBytes(sp, sl), t, m, p, dkLen, (r) => cbdata(cbID, r));
    },
    subtle_sha256_hex(p, l, _c, cbID) {
      subtle.SHA256Hex(readBytes(p, l), (s) => cbs(cbID, s));
    },
    subtle_hmac_sha512(kp, kl, _kc, dp, dl, _dc, cbID) {
      subtle.HMACSHA512(readBytes(kp, kl), readBytes(dp, dl), (r) => cbdata(cbID, r));
    },
    subtle_pbkdf2_sha512(pp, pl, _pc, sp, sl, _sc, iterations, dkLen, cbID) {
      subtle.PBKDF2SHA512(readStr(pp, pl), readBytes(sp, sl), iterations, dkLen, (r) => cbdata(cbID, r));
    },

    // --- signer ---
    signer_has_signer() { return signer.HasSigner() ? 1 : 0; },
    signer_has_mls() { return signer.HasMLS() ? 1 : 0; },
    signer_get_public_key(cbID) { signer.GetPublicKey((s) => cbs(cbID, s)); },
    signer_sign_event(p, l, _c, cbID) { signer.SignEvent(readStr(p, l), (s) => cbs(cbID, s)); },
    signer_get_shared_secret(p, l, _c, cbID) { signer.GetSharedSecret(readStr(p, l), (s) => cbs(cbID, s)); },
    signer_nip04_decrypt(pkp, pkl, _pkc, cp, cl, _cc, cbID) {
      signer.Nip04Decrypt(readStr(pkp, pkl), readStr(cp, cl), (s) => cbs(cbID, s));
    },
    signer_nip04_encrypt(pkp, pkl, _pkc, pp, pl, _pc, cbID) {
      signer.Nip04Encrypt(readStr(pkp, pkl), readStr(pp, pl), (s) => cbs(cbID, s));
    },
    signer_nip44_decrypt(pkp, pkl, _pkc, cp, cl, _cc, cbID) {
      signer.Nip44Decrypt(readStr(pkp, pkl), readStr(cp, cl), (s) => cbs(cbID, s));
    },
    signer_nip44_encrypt(pkp, pkl, _pkc, pp, pl, _pc, cbID) {
      signer.Nip44Encrypt(readStr(pkp, pkl), readStr(pp, pl), (s) => cbs(cbID, s));
    },
    signer_is_installed(cbID) { signer.IsInstalled((v) => cbb(cbID, v)); },
    signer_get_vault_status(cbID) { signer.GetVaultStatus((s) => cbs(cbID, s)); },
    signer_create_vault(p, l, _c, cbID) { signer.CreateVault(readStr(p, l), (v) => cbb(cbID, v)); },
    signer_unlock_vault(p, l, _c, cbID) { signer.UnlockVault(readStr(p, l), (v) => cbb(cbID, v)); },
    signer_lock_vault(cbID) { signer.LockVault(() => cb0(cbID)); },
    signer_list_identities(cbID) { signer.ListIdentities((s) => cbs(cbID, s)); },
    signer_add_identity(p, l, _c, cbID) { signer.AddIdentity(readStr(p, l), (v) => cbb(cbID, v)); },
    signer_nsec_login(p, l, _c, cbID) { signer.NsecLogin(readStr(p, l), (v) => cbb(cbID, v)); },
    signer_switch_identity(p, l, _c, cbID) { signer.SwitchIdentity(readStr(p, l), (v) => cbb(cbID, v)); },
    signer_export_vault(p, l, _c, cbID) { signer.ExportVault(readStr(p, l), (s) => cbs(cbID, s)); },
    signer_import_vault(p, l, _c, cbID) { signer.ImportVault(readStr(p, l), (v) => cbb(cbID, v)); },
    signer_remove_identity(p, l, _c, cbID) { signer.RemoveIdentity(readStr(p, l), (v) => cbb(cbID, v)); },
    signer_generate_mnemonic(cbID) { signer.GenerateMnemonic((s) => cbs(cbID, s)); },
    signer_validate_mnemonic(p, l, _c, cbID) { signer.ValidateMnemonic(readStr(p, l), (v) => cbb(cbID, v)); },
    signer_create_hd_vault(pp, pl, _pc, np, nl, _nc, cbID) {
      signer.CreateHDVault(readStr(pp, pl), readStr(np, nl), (s) => cbs(cbID, s));
    },
    signer_restore_hd_vault(pp, pl, _pc, mp, ml, _mc, np, nl, _nc, cbID) {
      signer.RestoreHDVault(readStr(pp, pl), readStr(mp, ml), readStr(np, nl), (v) => cbb(cbID, v));
    },
    signer_derive_identity(p, l, _c, cbID) { signer.DeriveIdentity(readStr(p, l), (s) => cbs(cbID, s)); },
    signer_get_mnemonic(cbID) { signer.GetMnemonic((s) => cbs(cbID, s)); },
    signer_probe_account(index, cbID) { signer.ProbeAccount(index, (s) => cbs(cbID, s)); },
    signer_is_hd(cbID) { signer.IsHD((v) => cbb(cbID, v)); },
    signer_reset_extension(cbID) { signer.ResetExtension((v) => cbb(cbID, v)); },
    signer_nwc_list(cbID) { signer.NwcList((s) => cbs(cbID, s)); },
    signer_nwc_add(up, ul, _uc, ap, al, _ac, createdAt, cbID) {
      signer.NwcAdd(readStr(up, ul), readStr(ap, al), Number(createdAt), (s) => cbs(cbID, s));
    },
    signer_nwc_remove(p, l, _c, cbID) { signer.NwcRemove(readStr(p, l), (v) => cbb(cbID, v)); },
    signer_nwc_build_request(ip, il, _ic, mp, ml, _mc, pp, pl, _pc, ep, el2, _ec, createdAt, cbID) {
      signer.NwcBuildRequest(readStr(ip, il), readStr(mp, ml), readStr(pp, pl), readStr(ep, el2), Number(createdAt), (s) => cbs(cbID, s));
    },
    signer_nwc_parse_response(ip, il, _ic, cp, cl, _cc, ep, el2, _ec, cbID) {
      signer.NwcParseResponse(readStr(ip, il), readStr(cp, cl), readStr(ep, el2), (s) => cbs(cbID, s));
    },

    // --- idb ---
    idb_set_enc_key(p, l, _c) { idb.SetEncKey(readStr(p, l)); },
    idb_open(cbID) { idb.Open(() => cb0(cbID)); },
    idb_save_event(p, l, _c, cbID) { idb.SaveEvent(readStr(p, l), (v) => cbb(cbID, v)); },
    idb_query_events(p, l, _c, cbID) { idb.QueryEvents(readStr(p, l), (s) => cbs(cbID, s)); },
    idb_save_dm(p, l, _c, cbID) { idb.SaveDM(readStr(p, l), (s) => cbs(cbID, s)); },
    idb_query_dms(pp, pl, _pc, limit, until, cbID) {
      idb.QueryDMs(readStr(pp, pl), limit, Number(until), (s) => cbs(cbID, s));
    },
    idb_get_conversation_list(cbID) { idb.GetConversationList((s) => cbs(cbID, s)); },
    idb_clear_dms_by_peer(p, l, _c, cbID) { idb.ClearDMsByPeer(readStr(p, l), () => cb0(cbID)); },
    idb_set_version(p, l, _c) { idb.SetVersion(readStr(p, l)); },
    idb_mls_save_group(gp, gl, _gc, sp, sl, _sc, cbID) {
      idb.MlsSaveGroup(readStr(gp, gl), readStr(sp, sl), () => cb0(cbID));
    },
    idb_mls_load_group(gp, gl, _gc, cbID) {
      idb.MlsLoadGroup(readStr(gp, gl), (s) => cbs(cbID, s));
    },
    idb_mls_list_groups(cbID) { idb.MlsListGroups((s) => cbs(cbID, s)); },
    idb_mls_save_kpp(p, l, _c, cbID) { idb.MlsSaveKPP(readStr(p, l), () => cb0(cbID)); },
    idb_mls_load_kpp(cbID) { idb.MlsLoadKPP((s) => cbs(cbID, s)); },

    // --- ext ---
    ext_storage_get(kp, kl, _kc, cbID) { ext.StorageGet(readStr(kp, kl), (s) => cbs(cbID, s)); },
    ext_storage_set(kp, kl, _kc, vp, vl, _vc) { ext.StorageSet(readStr(kp, kl), readStr(vp, vl)); },
    ext_storage_remove(kp, kl, _kc) { ext.StorageRemove(readStr(kp, kl)); },
    ext_on_message(_unused) {
      ext.OnMessage((method, params, tabID, respond) => {
        const reqID = hookNextReq++;
        hookResponses.set(reqID, respond);
        const m = writeStr(method);
        const p = writeStr(params);
        queueMicrotask(() => instance.exports.__hook_ext_on_message(reqID, m.ptr, m.len, p.ptr, p.len, tabID));
      });
    },
    ext_on_message_respond(reqID, p, l, _c) {
      const respond = hookResponses.get(reqID);
      if (respond) { hookResponses.delete(reqID); respond(readStr(p, l)); }
    },
    ext_send_message_to_tab(tabID, mp, ml, _mc) { ext.SendMessageToTab(tabID, readStr(mp, ml)); },
    ext_get_active_tab(cbID) { ext.GetActiveTab((id, url) => cbis(cbID, id, url)); },
    ext_console_log(p, l, _c) { console.log(readStr(p, l)); },
    ext_session_get(kp, kl, _kc, cbID) { ext.SessionGet(readStr(kp, kl), (s) => cbs(cbID, s)); },
    ext_session_set(kp, kl, _kc, vp, vl, _vc) { ext.SessionSet(readStr(kp, kl), readStr(vp, vl)); },
    ext_is_in_page() { return ext.IsInPage() ? 1 : 0; },

    // --- registry ---
    registry_set_seckey(p, l, _c) { self.$hooks = self.$hooks || {}; self.$hooks.seckey = readStr(p, l); },
    registry_seckey(op, ol) { setOutStr(op, ol, (self.$hooks && self.$hooks.seckey) || ''); },
    registry_set_pubkey(p, l, _c) { self.$hooks = self.$hooks || {}; self.$hooks.pubkey = readStr(p, l); },
    registry_pubkey(op, ol) { setOutStr(op, ol, (self.$hooks && self.$hooks.pubkey) || ''); },
    registry_set_has_key(v) { self.$hooks = self.$hooks || {}; self.$hooks.hasKey = v !== 0; },
    registry_has_key() { return (self.$hooks && self.$hooks.hasKey) ? 1 : 0; },

    registry_on_marmot_init(cbID) { self.$hooks = self.$hooks || {}; self.$hooks.marmotInit = (s) => cbs(cbID, s); },
    registry_on_marmot_send(cbID) { self.$hooks = self.$hooks || {}; self.$hooks.marmotSend = (r, c) => cbss(cbID, r, c); },
    registry_on_marmot_subscribe(cbID) { self.$hooks = self.$hooks || {}; self.$hooks.marmotSubscribe = () => cb0(cbID); },
    registry_on_marmot_publish_kp(cbID) { self.$hooks = self.$hooks || {}; self.$hooks.marmotPublishKP = (s) => cbs(cbID, s); },
    registry_on_marmot_list_groups(cbID) { self.$hooks = self.$hooks || {}; self.$hooks.marmotListGroups = (s) => cbs(cbID, s); },
    registry_on_save_dm_record(cbID) { self.$hooks = self.$hooks || {}; self.$hooks.saveDMRecord = (s) => cbs(cbID, s); },
    registry_on_broadcast_to_clients(cbID) { self.$hooks = self.$hooks || {}; self.$hooks.broadcastToClients = (s) => cbs(cbID, s); },
    registry_on_send_to_client(cbID) { self.$hooks = self.$hooks || {}; self.$hooks.sendToClient = (id, m) => cbss(cbID, id, m); },

    registry_on_encrypt_nip04() {
      self.$hooks = self.$hooks || {};
      self.$hooks.encryptNip04 = (pk, ct) => new Promise(resolve => {
        const reqID = hookNextReq++;
        hookResponses.set(reqID, resolve);
        const p = writeStr(pk); const c = writeStr(ct);
        queueMicrotask(() => instance.exports.__hook_encrypt_nip04(reqID, p.ptr, p.len, c.ptr, c.len));
      });
    },
    registry_on_encrypt_nip17() {
      self.$hooks = self.$hooks || {};
      self.$hooks.encryptNip17 = (pk, ct) => new Promise(resolve => {
        const reqID = hookNextReq++;
        hookResponses.set(reqID, resolve);
        const p = writeStr(pk); const c = writeStr(ct);
        queueMicrotask(() => instance.exports.__hook_encrypt_nip17(reqID, p.ptr, p.len, c.ptr, c.len));
      });
    },
    registry_on_decrypt_dm() {
      self.$hooks = self.$hooks || {};
      self.$hooks.decryptDM = (ev) => new Promise(resolve => {
        const reqID = hookNextReq++;
        hookResponses.set(reqID, resolve);
        const e = writeStr(ev);
        queueMicrotask(() => instance.exports.__hook_decrypt_dm(reqID, e.ptr, e.len));
      });
    },
    registry_hook_respond_s(reqID, p, l, _c) {
      const resolve = hookResponses.get(reqID);
      if (resolve) { hookResponses.delete(reqID); resolve(readStr(p, l)); }
    },
    registry_hook_respond_ss(reqID, p1, l1, _c1, p2, l2, _c2) {
      const resolve = hookResponses.get(reqID);
      if (resolve) { hookResponses.delete(reqID); resolve([readStr(p1, l1), readStr(p2, l2)]); }
    },

    registry_encrypt_nip04(pkp, pkl, _pkc, cp, cl, _cc, cbID) {
      if (self.$hooks && self.$hooks.encryptNip04) {
        self.$hooks.encryptNip04(readStr(pkp, pkl), readStr(cp, cl)).then((s) => cbs(cbID, s));
      }
    },
    registry_encrypt_nip17(pkp, pkl, _pkc, cp, cl, _cc, cbID) {
      if (self.$hooks && self.$hooks.encryptNip17) {
        self.$hooks.encryptNip17(readStr(pkp, pkl), readStr(cp, cl)).then(([a, b]) => cbss(cbID, a, b));
      }
    },
    registry_decrypt_dm(p, l, _c, cbID) {
      if (self.$hooks && self.$hooks.decryptDM) {
        self.$hooks.decryptDM(readStr(p, l)).then((s) => cbs(cbID, s));
      }
    },
    registry_make_dm_record(pp, pl, _pc, fp, fl, _fc, cp, cl, _cc, ts, prp, prl, _prc, ep, el2, _ec, op, ol) {
      // Build DM record JSON directly
      const peer = readStr(pp, pl), from = readStr(fp, fl), content = readStr(cp, cl);
      const proto = readStr(prp, prl), eid = readStr(ep, el2);
      const r = JSON.stringify({peer, from, content, ts: Number(ts), proto, eid});
      setOutStr(op, ol, r);
    },
    registry_marmot_init(p, l, _c) { if (self.$hooks && self.$hooks.marmotInit) self.$hooks.marmotInit(readStr(p, l)); },
    registry_marmot_send(rp, rl, _rc, cp, cl, _cc) { if (self.$hooks && self.$hooks.marmotSend) self.$hooks.marmotSend(readStr(rp, rl), readStr(cp, cl)); },
    registry_marmot_subscribe() { if (self.$hooks && self.$hooks.marmotSubscribe) self.$hooks.marmotSubscribe(); },
    registry_marmot_publish_kp(p, l, _c) { if (self.$hooks && self.$hooks.marmotPublishKP) self.$hooks.marmotPublishKP(readStr(p, l)); },
    registry_marmot_list_groups(p, l, _c) { if (self.$hooks && self.$hooks.marmotListGroups) self.$hooks.marmotListGroups(readStr(p, l)); },
    registry_save_dm_record(p, l, _c) { if (self.$hooks && self.$hooks.saveDMRecord) self.$hooks.saveDMRecord(readStr(p, l)); },
    registry_broadcast_to_clients(p, l, _c) { if (self.$hooks && self.$hooks.broadcastToClients) self.$hooks.broadcastToClients(readStr(p, l)); },
    registry_send_to_client(ip, il, _ic, mp, ml, _mc) { if (self.$hooks && self.$hooks.sendToClient) self.$hooks.sendToClient(readStr(ip, il), readStr(mp, ml)); },
    registry_has_hook(p, l, _c) { return (self.$hooks && self.$hooks[readStr(p, l)]) ? 1 : 0; },
    registry_load_module(p, l, _c, cbID) {
      // Module loading is moxiejs-specific; in WASM all modules are compiled in
      cb0(cbID);
    },

    // --- sw ---
    sw_on_install(cbID) { self.addEventListener('install', (e) => cbi(cbID, 0)); },
    sw_on_activate(cbID) { self.addEventListener('activate', (e) => cbi(cbID, 0)); },
    sw_on_fetch(cbID) { self.addEventListener('fetch', (e) => cbi(cbID, 0)); },
    sw_on_message(cbID) { self.addEventListener('message', (e) => cbi(cbID, 0)); },
    sw_wait_until(event, cbID) { /* simplified - actual impl needs event ref */ cb0(cbID); },
    sw_respond_with(event, resp) { /* needs event/response tracking */ },
    sw_respond_with_network(event) { /* pass-through */ },
    sw_respond_with_cache_first(event) { /* needs event tracking */ },
    sw_respond_with_network_first(event) { /* needs event tracking */ },
    sw_get_request_url(event, op, ol) { setOutStr(op, ol, ''); },
    sw_get_request_path(event, op, ol) { setOutStr(op, ol, ''); },
    sw_get_message_data(event, op, ol) { setOutStr(op, ol, ''); },
    sw_get_message_client_id(event, op, ol) { setOutStr(op, ol, ''); },
    sw_origin(op, ol) { setOutStr(op, ol, self.location ? self.location.origin : ''); },
    sw_skip_waiting() { if (self.skipWaiting) self.skipWaiting(); },
    sw_claim_clients(cbID) {
      if (self.clients && self.clients.claim) self.clients.claim().then(() => cb0(cbID));
      else cb0(cbID);
    },
    sw_match_clients(cbID) { /* simplified */ },
    sw_post_message(client, p, l, _c) { /* needs client handle tracking */ },
    sw_post_message_json(client, p, l, _c) { /* needs client handle tracking */ },
    sw_get_client_by_id(p, l, _c, cbID) { cbii(cbID, 0, 0); },
    sw_navigate(client, p, l, _c) { /* needs client handle tracking */ },
    sw_cache_open(p, l, _c, cbID) {
      caches.open(readStr(p, l)).then(() => cbi(cbID, 1));
    },
    sw_cache_add_all(cache, up, ul, _uc, count, cbID) {
      const urls = splitNull(up, ul, count);
      caches.open('v1').then(c => c.addAll(urls)).then(() => cb0(cbID));
    },
    sw_cache_from_manifests(cache, fp, fl, _fc, count, cbID) { cb0(cbID); },
    sw_cache_put(cache, up, ul, _uc, resp, cbID) { cb0(cbID); },
    sw_cache_match(p, l, _c, cbID) { cbi(cbID, 0); },
    sw_cache_delete(p, l, _c, cbID) {
      caches.delete(readStr(p, l)).then(() => cb0(cbID));
    },
    sw_fetch(p, l, _c, cbID) {
      fetch(readStr(p, l)).then(r => cbii(cbID, 1, r.ok ? 1 : 0)).catch(() => cbii(cbID, 0, 0));
    },
    sw_fetch_all(up, ul, _uc, count, eachID, doneID) {
      const urls = splitNull(up, ul, count);
      Promise.all(urls.map((u, i) => fetch(u).then(r => cbiii(eachID, i, 1, r.ok ? 1 : 0)).catch(() => cbiii(eachID, i, 0, 0))))
        .then(() => cb0(doneID));
    },
    sw_response_ok(resp) { return 0; },
    sw_sse_connect(p, l, _c, cbID) {
      const es = new EventSource(readStr(p, l));
      es.onmessage = (e) => cbs(cbID, e.data);
      return 1;
    },
    sw_sse_close(id) { /* needs handle tracking */ },
    sw_set_timeout(ms, cbID) { return setTimeout(() => cb0(cbID), ms); },
    sw_clear_timeout(t) { clearTimeout(t); },
    sw_now_seconds() { return BigInt(Math.floor(Date.now() / 1000)); },
    sw_now_millis() { return BigInt(Date.now()); },
    sw_log(p, l, _c) { console.log(readStr(p, l)); },

    // --- webgl ---
    webgl_CreateContext(canvasId) { return webgl.CreateContext(canvasId); },
    webgl_CreateContextFromHandle(el) { return webgl.CreateContextFromHandle(el); },
    webgl_DeleteContext(ctx) { webgl.DeleteContext(ctx); },
    webgl_Enable(ctx, cap) { webgl.Enable(ctx, cap); },
    webgl_Disable(ctx, cap) { webgl.Disable(ctx, cap); },
    webgl_Viewport(ctx, x, y, w, h) { webgl.Viewport(ctx, x, y, w, h); },
    webgl_Scissor(ctx, x, y, w, h) { webgl.Scissor(ctx, x, y, w, h); },
    webgl_BlendFunc(ctx, src, dst) { webgl.BlendFunc(ctx, src, dst); },
    webgl_BlendFuncSeparate(ctx, sr, dr, sa, da) { webgl.BlendFuncSeparate(ctx, sr, dr, sa, da); },
    webgl_BlendEquation(ctx, mode) { webgl.BlendEquation(ctx, mode); },
    webgl_DepthFunc(ctx, fn) { webgl.DepthFunc(ctx, fn); },
    webgl_DepthMask(ctx, flag) { webgl.DepthMask(ctx, flag); },
    webgl_ClearColor(ctx, r, g, b, a) { webgl.ClearColor(ctx, r, g, b, a); },
    webgl_ClearDepth(ctx, d) { webgl.ClearDepth(ctx, d); },
    webgl_Clear(ctx, mask) { webgl.Clear(ctx, mask); },
    webgl_Flush(ctx) { webgl.Flush(ctx); },
    webgl_Finish(ctx) { webgl.Finish(ctx); },
    webgl_GetError(ctx) { return webgl.GetError(ctx); },
    webgl_GetInteger(ctx, pname) { return webgl.GetInteger(ctx, pname); },
    webgl_IsEnabled(ctx, cap) { return webgl.IsEnabled(ctx, cap); },
    webgl_PixelStorei(ctx, pname, param) { webgl.PixelStorei(ctx, pname, param); },
    webgl_ReadPixels(ctx, x, y, w, h, fmt, typ, bufPtr, bufLen) { webgl.ReadPixels(ctx, x, y, w, h, fmt, typ, bufPtr, bufLen); },
    webgl_CreateBuffer(ctx) { return webgl.CreateBuffer(ctx); },
    webgl_DeleteBuffer(ctx, buf) { webgl.DeleteBuffer(ctx, buf); },
    webgl_BindBuffer(ctx, target, buf) { webgl.BindBuffer(ctx, target, buf); },
    webgl_BufferData(ctx, target, ptr, len, usage) { webgl.BufferData(ctx, target, ptr, len, usage); },
    webgl_BufferSubData(ctx, target, off, ptr, len) { webgl.BufferSubData(ctx, target, off, ptr, len); },
    webgl_CreateTexture(ctx) { return webgl.CreateTexture(ctx); },
    webgl_DeleteTexture(ctx, tex) { webgl.DeleteTexture(ctx, tex); },
    webgl_BindTexture(ctx, target, tex) { webgl.BindTexture(ctx, target, tex); },
    webgl_ActiveTexture(ctx, unit) { webgl.ActiveTexture(ctx, unit); },
    webgl_TexParameteri(ctx, target, pname, param) { webgl.TexParameteri(ctx, target, pname, param); },
    webgl_TexImage2D(ctx, target, lvl, ifmt, w, h, border, fmt, typ, ptr, len) { webgl.TexImage2D(ctx, target, lvl, ifmt, w, h, border, fmt, typ, ptr, len); },
    webgl_TexStorage2D(ctx, target, lvls, ifmt, w, h) { webgl.TexStorage2D(ctx, target, lvls, ifmt, w, h); },
    webgl_TexSubImage2D(ctx, target, lvl, xo, yo, w, h, fmt, typ, ptr, len) { webgl.TexSubImage2D(ctx, target, lvl, xo, yo, w, h, fmt, typ, ptr, len); },
    webgl_CopyTexSubImage2D(ctx, target, lvl, xo, yo, x, y, w, h) { webgl.CopyTexSubImage2D(ctx, target, lvl, xo, yo, x, y, w, h); },
    webgl_GenerateMipmap(ctx, target) { webgl.GenerateMipmap(ctx, target); },
    webgl_CreateFramebuffer(ctx) { return webgl.CreateFramebuffer(ctx); },
    webgl_DeleteFramebuffer(ctx, fbo) { webgl.DeleteFramebuffer(ctx, fbo); },
    webgl_BindFramebuffer(ctx, target, fbo) { webgl.BindFramebuffer(ctx, target, fbo); },
    webgl_FramebufferTexture2D(ctx, target, att, texTarget, tex, lvl) { webgl.FramebufferTexture2D(ctx, target, att, texTarget, tex, lvl); },
    webgl_CheckFramebufferStatus(ctx, target) { return webgl.CheckFramebufferStatus(ctx, target); },
    webgl_InvalidateFramebuffer(ctx, target, att) { webgl.InvalidateFramebuffer(ctx, target, att); },
    webgl_CreateRenderbuffer(ctx) { return webgl.CreateRenderbuffer(ctx); },
    webgl_DeleteRenderbuffer(ctx, rbo) { webgl.DeleteRenderbuffer(ctx, rbo); },
    webgl_BindRenderbuffer(ctx, target, rbo) { webgl.BindRenderbuffer(ctx, target, rbo); },
    webgl_RenderbufferStorage(ctx, target, ifmt, w, h) { webgl.RenderbufferStorage(ctx, target, ifmt, w, h); },
    webgl_FramebufferRenderbuffer(ctx, target, att, rbtarget, rbo) { webgl.FramebufferRenderbuffer(ctx, target, att, rbtarget, rbo); },
    webgl_CreateShader(ctx, typ) { return webgl.CreateShader(ctx, typ); },
    webgl_DeleteShader(ctx, sh) { webgl.DeleteShader(ctx, sh); },
    webgl_ShaderSource(ctx, sh, ptr, len) { webgl.ShaderSource(ctx, sh, ptr, len); },
    webgl_CompileShader(ctx, sh) { webgl.CompileShader(ctx, sh); },
    webgl_GetShaderParameter(ctx, sh, pname) { return webgl.GetShaderParameter(ctx, sh, pname); },
    webgl_GetShaderInfoLog(ctx, sh, ptr, len) { return webgl.GetShaderInfoLog(ctx, sh, ptr, len); },
    webgl_CreateProgram(ctx) { return webgl.CreateProgram(ctx); },
    webgl_DeleteProgram(ctx, prog) { webgl.DeleteProgram(ctx, prog); },
    webgl_AttachShader(ctx, prog, sh) { webgl.AttachShader(ctx, prog, sh); },
    webgl_LinkProgram(ctx, prog) { webgl.LinkProgram(ctx, prog); },
    webgl_UseProgram(ctx, prog) { webgl.UseProgram(ctx, prog); },
    webgl_GetProgramParameter(ctx, prog, pname) { return webgl.GetProgramParameter(ctx, prog, pname); },
    webgl_GetProgramInfoLog(ctx, prog, ptr, len) { return webgl.GetProgramInfoLog(ctx, prog, ptr, len); },
    webgl_BindAttribLocation(ctx, prog, idx, ptr, len) { webgl.BindAttribLocation(ctx, prog, idx, ptr, len); },
    webgl_GetUniformLocation(ctx, prog, ptr, len) { return webgl.GetUniformLocation(ctx, prog, ptr, len); },
    webgl_Uniform1f(ctx, loc, v) { webgl.Uniform1f(ctx, loc, v); },
    webgl_Uniform2f(ctx, loc, v0, v1) { webgl.Uniform2f(ctx, loc, v0, v1); },
    webgl_Uniform3f(ctx, loc, v0, v1, v2) { webgl.Uniform3f(ctx, loc, v0, v1, v2); },
    webgl_Uniform4f(ctx, loc, v0, v1, v2, v3) { webgl.Uniform4f(ctx, loc, v0, v1, v2, v3); },
    webgl_Uniform1i(ctx, loc, v) { webgl.Uniform1i(ctx, loc, v); },
    webgl_EnableVertexAttribArray(ctx, idx) { webgl.EnableVertexAttribArray(ctx, idx); },
    webgl_DisableVertexAttribArray(ctx, idx) { webgl.DisableVertexAttribArray(ctx, idx); },
    webgl_VertexAttribPointer(ctx, idx, size, typ, norm, stride, off) { webgl.VertexAttribPointer(ctx, idx, size, typ, norm, stride, off); },
    webgl_DrawArrays(ctx, mode, first, count) { webgl.DrawArrays(ctx, mode, first, count); },
    webgl_DrawElements(ctx, mode, count, typ, off) { webgl.DrawElements(ctx, mode, count, typ, off); },

    // --- text ---
    text_Measure(fp, fl, sz, tp, tl, wp, hp) { text.Measure(fp, fl, sz, tp, tl, wp, hp); },
    text_Render(fp, fl, sz, tp, tl, mw, dp, dc) { return text.Render(fp, fl, sz, tp, tl, mw, dp, dc); },

    // --- app ---
    app_CreateFullscreenCanvas() { app.CreateFullscreenCanvas(); },
    app_CreateWebGLContext() { return app.CreateWebGLContext(); },
    app_GetDevicePixelRatio() { return app.GetDevicePixelRatio(); },
    app_GetCanvasCSSSize(wPtr, hPtr) { app.GetCanvasCSSSize(wPtr, hPtr); },
    app_SetCanvasBacking(w, h) { app.SetCanvasBacking(w, h); },
    app_RequestRAF() { app.RequestRAF(); },
    app_SetCursor(ptr, len) { app.SetCursor(ptr, len); },
    app_SetTitle(ptr, len) { app.SetTitle(ptr, len); },
    app_SetFullscreen(full) { app.SetFullscreen(full); },
    app_RegisterPointerEvents() { app.RegisterPointerEvents(); },
    app_RegisterKeyEvents() { app.RegisterKeyEvents(); },
    app_RegisterResizeEvents() { app.RegisterResizeEvents(); },
    // Spawn bridge: channels are SPSC rings over SharedArrayBuffer, and a
    // domain is a dedicated Worker running the same module. The module only
    // ever sees an int32 handle, so the host keeps the table.
    channel_create(slotSize, slotCount) {
      const h = next++;
      handles.set(h, sabChannel.create(slotSize, slotCount));
      return h;
    },
    channel_send(h, ptr, len) {
      const sab = handles.get(h);
      if (sab) sabChannel.send(sab, readBytes(ptr, len));
    },
    channel_recv(h, ptr, cap) {
      const sab = handles.get(h);
      if (!sab) return -1;
      const dst = new Uint8Array(memory.buffer, ptr, cap);
      return sabChannel.recv(sab, dst);
    },
    channel_close(h) {
      const sab = handles.get(h);
      if (sab) { sabChannel.close(sab); handles.delete(h); }
    },
    is_worker_context() { return isWorker; },
    spawn_domain(fnIdx, argPtr, argLen, chanHandlesPtr, nChans) {
      // Workers are started from the host: the module cannot create one.
      // The entry index and the bound channel handles are handed to the
      // worker over its own message port so the child can bind the same
      // rings from the far end. A spawned domain does not spawn again.
      if (isWorker) return;
      const args = readBytes(argPtr, argLen);
      const chans = [];
      const dv = new DataView(memory.buffer);
      for (let i = 0; i < nChans; i++) {
        const h = dv.getInt32(chanHandlesPtr + i * 4, true);
        if (handles.has(h)) chans.push({ handle: h, sab: handles.get(h) });
      }
      spawnWorker(fnIdx, args, chans);
    },
  };
}

// buildWorkerBridge returns the bridge for a spawned domain: the same
// functions as the page, over the channel rings the parent handed over.
export function buildWorkerBridge(channels) {
  return buildBridge({ channels, isWorker: true });
}

// buildHostImports returns the non-bridge import object every Moxie wasm
// module needs: the WASI preview1 surface it actually calls, plus the env
// shims the stdlib closure pulls in. mem is a getter for the module's memory,
// not the memory itself, because these imports are built before instantiation
// completes and the buffer only exists once the instance is returned.
export function buildHostImports(mem) {
  return {
    wasi_snapshot_preview1: {
      fd_write(fd, iovs, iovs_len, nwritten) {
        const dv = new DataView(mem().buffer);
        let totalWritten = 0;
        for (let i = 0; i < iovs_len; i++) {
          const ptr = dv.getUint32(iovs + i * 8, true);
          const len = dv.getUint32(iovs + i * 8 + 4, true);
          const s = decoder.decode(new Uint8Array(mem().buffer, ptr, len));
          // The module runs in a Worker, so its output cannot reach the page
          // through the console alone: forward each write over the port and
          // let the page decide where stdout and stderr go.
          globalThis.postMessage({ kind: fd === 1 ? 'stdout' : 'stderr', text: s });
          totalWritten += len;
        }
        dv.setUint32(nwritten, totalWritten, true);
        return 0;
      },
      // A domain has no stdin: report EOF so runtime.buffered returns 0 and
      // os.Stdin.Read yields instead of blocking on a read that never comes.
      fd_read(fd, iovs, iovs_len, nread) {
        const dv = new DataView(mem().buffer);
        dv.setUint32(nread, 0, true);
        return 0;
      },
      clock_time_get(clockID, precision, time) {
        const now = clockID === 1
          ? performance.now() * 1_000_000
          : Date.now() * 1_000_000;
        const dv = new DataView(mem().buffer);
        dv.setBigInt64(time, BigInt(Math.floor(now)), true);
        return 0;
      },
    },
    // Host shims for pkg/mxutil. Those functions are declared with
    // //export, which makes them imports under wasm, and mxutil arrives in
    // an application's closure because the standard library's errors
    // package uses its GrowCap helper. A browser app has no filesystem, so
    // these report failure rather than pretending to work; they are only
    // reached if something calls them, which nothing in a page does.
    //
    // Anything else the module imports from env is a host shim that no page
    // code reaches (the whole symbol is pulled in by the stdlib closure).
    // Resolving unknown names to a no-op keeps the module loadable; naming
    // each one would mean editing this file every time the closure shifts.
    env: new Proxy({
      mxc_readfile: () => 0,
      mxc_filesize: () => -1,
      mxc_getenv: () => 0,
      mxc_listdir: () => 0,
      mxc_writefile: () => -1,
      mxc_mkdir: () => -1,
      // libc write, pulled in by the compiler-side C shims.
      write: () => -1,
      // syscall.Exit has no process to terminate in a browser. Report the
      // code through the host's error path so it is visible, not silent.
      'syscall.Exit': (code) => { throw new Error(`exit ${code | 0}`); },
    }, {
      get(target, prop) {
        return prop in target ? target[prop] : (() => 0);
      },
    }),
  };
}

// spawnWorker starts a new domain for a Moxie spawn. The child runs the same
// module in a dedicated Worker; the module cannot create one itself, and the
// bound channel rings travel to the child as SharedArrayBuffers, which is the
// only inter-domain state there is.
function spawnWorker(fnIdx, argBytes, chans) {
  startDomain({
    kind: 'entry',
    wasm: globalThis.__moxie_wasm_url,
    fnIdx,
    args: argBytes,
    chans: chans.map(c => ({ handle: c.handle, sab: c.sab })),
  });
}

// startDomain creates the Worker that runs a Moxie domain and posts it its
// boot message. A domain always runs in its own Worker: channel receives
// block in Atomics.wait, which the browser forbids on the page thread, and a
// domain that spawns must itself be able to create a Worker.
export function startDomain(boot, onSettled) {
  const w = new Worker(new URL('./worker-host.mjs', import.meta.url), { type: 'module' });
  w.addEventListener('message', (e) => {
    const m = e.data;
    if (!m) return;
    if (m.kind === 'stdout') console.log(m.text);
    else if (m.kind === 'stderr') console.error(m.text);
    else if (m.kind === 'done') { w.terminate(); onSettled && onSettled(null); }
    else if (m.kind === 'error') { w.terminate(); onSettled && onSettled(new Error(m.error)); }
  });
  w.addEventListener('error', (e) => {
    onSettled && onSettled(new Error(String(e.message || e.error || e)));
  });
  w.postMessage(boot);
  return w;
}

// setHostModule points the host-side callbacks at a freshly instantiated
// module. Called by the Worker host once its instance exists, so reverse
// callbacks (__cb*) reach the right instance.
export function setHostModule(inst) {
  instance = inst;
  memory = inst.exports.memory;
}


