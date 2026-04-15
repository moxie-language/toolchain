// TinyJS Runtime — Poly1305 MAC (RFC 8439 §2.5)
// Uses JS BigInt for 130-bit modular arithmetic.

import { Slice } from './builtin.mjs';

const P = (1n << 130n) - 5n; // 2^130 - 5

function sliceToU8(s) {
  if (s instanceof Uint8Array) return s;
  if (typeof s === 'string') return new TextEncoder().encode(s);
  const u = new Uint8Array(s.$length);
  for (let i = 0; i < s.$length; i++) u[i] = s.$array[s.$offset + i];
  return u;
}

function u8ToSlice(u8) {
  const arr = new Array(u8.length);
  for (let i = 0; i < u8.length; i++) arr[i] = u8[i];
  return new Slice(arr, 0, u8.length, u8.length);
}

function le128(buf, off, len) {
  let n = 0n;
  for (let i = 0; i < len; i++) {
    n |= BigInt(buf[off + i]) << BigInt(i * 8);
  }
  return n;
}

function clamp(r) {
  // RFC 8439 §2.5.2: clamp r
  r &= 0x0ffffffc0ffffffc0ffffffc0fffffffn;
  return r;
}

function poly1305(key, data) {
  // r = clamp(key[0:16]), s = key[16:32]
  const r = clamp(le128(key, 0, 16));
  const s = le128(key, 16, 16);

  let acc = 0n;
  let off = 0;
  while (off < data.length) {
    const blockLen = Math.min(16, data.length - off);
    let n = le128(data, off, blockLen);
    // Add high bit: 2^(8*blockLen)
    n |= 1n << BigInt(blockLen * 8);
    acc = ((acc + n) * r) % P;
    off += blockLen;
  }

  // tag = (acc + s) mod 2^128
  const tag = (acc + s) & ((1n << 128n) - 1n);

  // Encode as 16-byte little-endian
  const out = new Uint8Array(16);
  let v = tag;
  for (let i = 0; i < 16; i++) {
    out[i] = Number(v & 0xffn);
    v >>= 8n;
  }
  return out;
}

export function MAC(key, data) {
  const k = sliceToU8(key);
  const d = sliceToU8(data);
  return u8ToSlice(poly1305(k, d));
}

export function Verify(key, data, tag) {
  const k = sliceToU8(key);
  const d = sliceToU8(data);
  const t = sliceToU8(tag);
  const computed = poly1305(k, d);
  if (computed.length !== t.length) return false;
  // Constant-time comparison
  let diff = 0;
  for (let i = 0; i < 16; i++) diff |= computed[i] ^ t[i];
  return diff === 0;
}
