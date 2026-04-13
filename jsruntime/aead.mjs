// TinyJS Runtime — XChaCha20-Poly1305 AEAD for IDB encryption at rest.
// Key is set once per session; all IDB values pass through seal/open.

import { MAC as poly1305MAC } from './poly1305.mjs';

let _key = null; // Uint8Array(32) or null

export function setKey(keyU8) { _key = keyU8; }
export function hasKey() { return _key !== null; }

// ── ChaCha20 core (uint32 only — no uint64 issues) ──

function _le32(b, i) { return (b[i] | b[i+1]<<8 | b[i+2]<<16 | b[i+3]<<24) >>> 0; }

function _qr(s, a, b, c, d) {
  s[a] = (s[a] + s[b]) >>> 0; s[d] ^= s[a]; s[d] = ((s[d] << 16) | (s[d] >>> 16)) >>> 0;
  s[c] = (s[c] + s[d]) >>> 0; s[b] ^= s[c]; s[b] = ((s[b] << 12) | (s[b] >>> 20)) >>> 0;
  s[a] = (s[a] + s[b]) >>> 0; s[d] ^= s[a]; s[d] = ((s[d] << 8) | (s[d] >>> 24)) >>> 0;
  s[c] = (s[c] + s[d]) >>> 0; s[b] ^= s[c]; s[b] = ((s[b] << 7) | (s[b] >>> 25)) >>> 0;
}

function _rounds(s) {
  for (let i = 0; i < 10; i++) {
    _qr(s,0,4,8,12); _qr(s,1,5,9,13); _qr(s,2,6,10,14); _qr(s,3,7,11,15);
    _qr(s,0,5,10,15); _qr(s,1,6,11,12); _qr(s,2,7,8,13); _qr(s,3,4,9,14);
  }
}

function _initState(key) {
  const s = new Uint32Array(16);
  s[0]=0x61707865; s[1]=0x3320646e; s[2]=0x79622d32; s[3]=0x6b206574;
  for (let i = 0; i < 8; i++) s[4+i] = _le32(key, i*4);
  return s;
}

function _hchacha20(key, nonce) {
  const s = _initState(key);
  for (let i = 0; i < 4; i++) s[12+i] = _le32(nonce, i*4);
  _rounds(s);
  const out = new Uint8Array(32);
  for (let i = 0; i < 4; i++) {
    out[i*4] = s[i]&0xff; out[i*4+1]=(s[i]>>>8)&0xff;
    out[i*4+2]=(s[i]>>>16)&0xff; out[i*4+3]=(s[i]>>>24)&0xff;
  }
  for (let i = 0; i < 4; i++) {
    const j = i + 12;
    out[16+i*4] = s[j]&0xff; out[16+i*4+1]=(s[j]>>>8)&0xff;
    out[16+i*4+2]=(s[j]>>>16)&0xff; out[16+i*4+3]=(s[j]>>>24)&0xff;
  }
  return out;
}

function _chacha20Block(state) {
  const s = new Uint32Array(state);
  _rounds(s);
  const out = new Uint8Array(64);
  for (let i = 0; i < 16; i++) {
    const v = (s[i] + state[i]) >>> 0;
    out[i*4] = v&0xff; out[i*4+1]=(v>>>8)&0xff; out[i*4+2]=(v>>>16)&0xff; out[i*4+3]=(v>>>24)&0xff;
  }
  return out;
}

function _xorAt(key, nonce12, counter, data) {
  const state = _initState(key);
  state[12] = counter;
  for (let i = 0; i < 3; i++) state[13+i] = _le32(nonce12, i*4);
  const out = new Uint8Array(data.length);
  let pos = 0;
  while (pos < data.length) {
    const block = _chacha20Block(state);
    state[12]++;
    const n = Math.min(data.length - pos, 64);
    for (let i = 0; i < n; i++) out[pos+i] = data[pos+i] ^ block[i];
    pos += n;
  }
  return out;
}

// ── AEAD ──

function _buildMacInput(ct) {
  const padLen = (16 - (ct.length % 16)) % 16;
  const mi = new Uint8Array(ct.length + padLen + 16);
  mi.set(ct);
  const tOff = ct.length + padLen;
  mi[tOff+8] = ct.length & 0xff;
  mi[tOff+9] = (ct.length >>> 8) & 0xff;
  mi[tOff+10] = (ct.length >>> 16) & 0xff;
  mi[tOff+11] = (ct.length >>> 24) & 0xff;
  return mi;
}

function _polyKey(subkey, chaNonce) {
  return _xorAt(subkey, chaNonce, 0, new Uint8Array(32));
}

// seal: returns Uint8Array(nonce24 ‖ ciphertext ‖ tag16)
export function sealBytes(key, plaintext) {
  const nonce = new Uint8Array(24);
  crypto.getRandomValues(nonce);
  const subkey = _hchacha20(key, nonce.subarray(0, 16));
  const chaN = new Uint8Array(12);
  chaN.set(nonce.subarray(16, 24), 4);
  const pk = _polyKey(subkey, chaN);
  const ct = _xorAt(subkey, chaN, 1, plaintext);
  const mi = _buildMacInput(ct);
  const tag = poly1305MAC(pk, mi);
  const out = new Uint8Array(24 + ct.length + 16);
  out.set(nonce, 0);
  out.set(ct, 24);
  // tag is a Slice from poly1305.mjs — convert
  for (let i = 0; i < 16; i++) out[24 + ct.length + i] = tag.$array[tag.$offset + i];
  return out;
}

// open: returns Uint8Array plaintext or null
export function openBytes(key, sealed) {
  if (sealed.length < 40) return null;
  const nonce = sealed.subarray(0, 24);
  const ct = sealed.subarray(24, sealed.length - 16);
  const tagGot = sealed.subarray(sealed.length - 16);
  const subkey = _hchacha20(key, nonce.subarray(0, 16));
  const chaN = new Uint8Array(12);
  chaN.set(nonce.subarray(16, 24), 4);
  const pk = _polyKey(subkey, chaN);
  const mi = _buildMacInput(ct);
  const tagExp = poly1305MAC(pk, mi);
  let diff = 0;
  for (let i = 0; i < 16; i++) diff |= tagGot[i] ^ tagExp.$array[tagExp.$offset + i];
  if (diff !== 0) return null;
  return _xorAt(subkey, chaN, 1, ct);
}

// String convenience: seal to base64, open from base64.
const _enc = typeof TextEncoder !== 'undefined' ? new TextEncoder() : null;
const _dec = typeof TextDecoder !== 'undefined' ? new TextDecoder() : null;

export function sealStr(str) {
  if (!_key) return str;
  const pt = _enc.encode(str);
  const sealed = sealBytes(_key, pt);
  return btoa(String.fromCharCode.apply(null, sealed));
}

export function openStr(b64) {
  if (!_key || !b64) return b64;
  // Detect unencrypted data (starts with '{' or '[' — JSON)
  if (b64.charAt(0) === '{' || b64.charAt(0) === '[') return b64;
  try {
    const raw = atob(b64);
    const sealed = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) sealed[i] = raw.charCodeAt(i);
    const pt = openBytes(_key, sealed);
    if (pt === null) return '';
    return _dec.decode(pt);
  } catch (e) {
    // Not base64 or decryption failed — return as-is (migration path)
    return b64;
  }
}
