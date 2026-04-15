// p256.mjs — NIST P-256 ECDH + ECDSA using JS BigInt.
// Short-Weierstrass curve y² = x³ - 3x + b over GF(p).
// Used by MLS cipher suite 0x0001 (DHKEM-P256, ECDSA-P256-SHA256).

import { Slice } from './builtin.mjs';

// --- Constants (FIPS 186-4, §D.1.2.3) ---

const P = 0xffffffff00000001000000000000000000000000ffffffffffffffffffffffffn;
const N = 0xffffffff00000000ffffffffffffffffbce6faada7179e84f3b9cac2fc632551n;
const B = 0x5ac635d8aa3a93e7b3ebbd55769886bc651d06b0cc53b0f63bce3c3e27d2604bn;
const Gx = 0x6b17d1f2e12c4247f8bce6e563a440f277037d812deb33a0f4a13945d898c296n;
const Gy = 0x4fe342e2fe1a7f9b8ee7eb4a7c0f9e162bce33576b315ececbb6406837bf51f5n;

// --- Field arithmetic mod P ---

function modP(x) { return ((x % P) + P) % P; }

function modPow(base, exp, m) {
  let r = 1n;
  base = ((base % m) + m) % m;
  while (exp > 0n) {
    if (exp & 1n) r = r * base % m;
    exp >>= 1n;
    base = base * base % m;
  }
  return r;
}

function modInv(a, m) { return modPow(a, m - 2n, m); }

// --- Point ops in Jacobian coordinates [X, Y, Z] where x=X/Z², y=Y/Z³ ---
// Identity: Z = 0.

function jDouble(p) {
  const [X1, Y1, Z1] = p;
  if (Z1 === 0n) return p;
  // a = -3 optimization (NIST curves).
  const A = modP((X1 - Z1 * Z1 % P) * ((X1 + Z1 * Z1 % P) % P));
  const M = modP(A + A + A);
  const Y2 = modP(Y1 * Y1);
  const S = modP(4n * X1 * Y2);
  const X3 = modP(M * M - 2n * S);
  const Y3 = modP(M * (S - X3) - 8n * Y2 * Y2);
  const Z3 = modP(2n * Y1 * Z1);
  return [X3, Y3, Z3];
}

function jAdd(p, q) {
  const [X1, Y1, Z1] = p;
  const [X2, Y2, Z2] = q;
  if (Z1 === 0n) return q;
  if (Z2 === 0n) return p;
  const Z1Z1 = modP(Z1 * Z1);
  const Z2Z2 = modP(Z2 * Z2);
  const U1 = modP(X1 * Z2Z2);
  const U2 = modP(X2 * Z1Z1);
  const S1 = modP(Y1 * Z2 * Z2Z2);
  const S2 = modP(Y2 * Z1 * Z1Z1);
  const H = modP(U2 - U1);
  const R = modP(S2 - S1);
  if (H === 0n) {
    if (R === 0n) return jDouble(p);
    return [0n, 0n, 0n]; // identity (shouldn't happen for distinct valid points)
  }
  const HH = modP(H * H);
  const HHH = modP(H * HH);
  const V = modP(U1 * HH);
  const X3 = modP(R * R - HHH - 2n * V);
  const Y3 = modP(R * (V - X3) - S1 * HHH);
  const Z3 = modP(Z1 * Z2 * H);
  return [X3, Y3, Z3];
}

function scalarMult(k, p) {
  let r = [0n, 0n, 0n];
  let q = p;
  k = ((k % N) + N) % N;
  while (k > 0n) {
    if (k & 1n) r = jAdd(r, q);
    q = jDouble(q);
    k >>= 1n;
  }
  return r;
}

function toAffine(p) {
  const [X, Y, Z] = p;
  if (Z === 0n) return null;
  const zInv = modInv(Z, P);
  const zInv2 = modP(zInv * zInv);
  const zInv3 = modP(zInv2 * zInv);
  return [modP(X * zInv2), modP(Y * zInv3)];
}

const G = [Gx, Gy, 1n];

// --- Encoding ---

function i2osp(x, len) {
  const out = new Uint8Array(len);
  for (let i = len - 1; i >= 0; i--) {
    out[i] = Number(x & 0xffn);
    x >>= 8n;
  }
  return out;
}

function os2ip(b) {
  let x = 0n;
  for (let i = 0; i < b.length; i++) x = (x << 8n) | BigInt(b[i]);
  return x;
}

// Uncompressed SEC1: 0x04 || X(32) || Y(32) = 65 bytes.
function encodeUncompressed(p) {
  const aff = toAffine(p);
  if (!aff) return new Uint8Array(65);
  const out = new Uint8Array(65);
  out[0] = 0x04;
  out.set(i2osp(aff[0], 32), 1);
  out.set(i2osp(aff[1], 32), 33);
  return out;
}

function decodeUncompressed(u8) {
  if (u8.length !== 65 || u8[0] !== 0x04) return null;
  const x = os2ip(u8.subarray(1, 33));
  const y = os2ip(u8.subarray(33, 65));
  if (x >= P || y >= P) return null;
  // Verify point is on curve: y² = x³ - 3x + b
  const lhs = modP(y * y);
  const rhs = modP(x * x * x - 3n * x + B);
  if (lhs !== rhs) return null;
  return [x, y, 1n];
}

// --- Slice interop ---

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

// --- SHA-256 (minimal, for ECDSA) ---

function sha256(msg) {
  const H = new Uint32Array([
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
    0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
  ]);
  const K = new Uint32Array([
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
  ]);
  const bitLen = BigInt(msg.length) * 8n;
  const padLen = ((msg.length + 9 + 63) >> 6) << 6;
  const buf = new Uint8Array(padLen);
  buf.set(msg);
  buf[msg.length] = 0x80;
  for (let i = 0; i < 8; i++) buf[padLen - 1 - i] = Number((bitLen >> BigInt(8 * i)) & 0xffn);
  const W = new Uint32Array(64);
  for (let off = 0; off < padLen; off += 64) {
    for (let i = 0; i < 16; i++) {
      W[i] = (buf[off + 4*i] << 24) | (buf[off + 4*i+1] << 16) | (buf[off + 4*i+2] << 8) | buf[off + 4*i+3];
      W[i] >>>= 0;
    }
    for (let i = 16; i < 64; i++) {
      const s0 = ((W[i-15] >>> 7) | (W[i-15] << 25)) ^ ((W[i-15] >>> 18) | (W[i-15] << 14)) ^ (W[i-15] >>> 3);
      const s1 = ((W[i-2] >>> 17) | (W[i-2] << 15)) ^ ((W[i-2] >>> 19) | (W[i-2] << 13)) ^ (W[i-2] >>> 10);
      W[i] = (W[i-16] + s0 + W[i-7] + s1) >>> 0;
    }
    let [a, b, c, d, e, f, g, h] = H;
    for (let i = 0; i < 64; i++) {
      const S1 = ((e >>> 6) | (e << 26)) ^ ((e >>> 11) | (e << 21)) ^ ((e >>> 25) | (e << 7));
      const ch = (e & f) ^ (~e & g);
      const t1 = (h + S1 + ch + K[i] + W[i]) >>> 0;
      const S0 = ((a >>> 2) | (a << 30)) ^ ((a >>> 13) | (a << 19)) ^ ((a >>> 22) | (a << 10));
      const mj = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (S0 + mj) >>> 0;
      h = g; g = f; f = e; e = (d + t1) >>> 0; d = c; c = b; b = a; a = (t1 + t2) >>> 0;
    }
    H[0] = (H[0] + a) >>> 0;
    H[1] = (H[1] + b) >>> 0;
    H[2] = (H[2] + c) >>> 0;
    H[3] = (H[3] + d) >>> 0;
    H[4] = (H[4] + e) >>> 0;
    H[5] = (H[5] + f) >>> 0;
    H[6] = (H[6] + g) >>> 0;
    H[7] = (H[7] + h) >>> 0;
  }
  const out = new Uint8Array(32);
  for (let i = 0; i < 8; i++) {
    out[4*i+0] = (H[i] >>> 24) & 0xff;
    out[4*i+1] = (H[i] >>> 16) & 0xff;
    out[4*i+2] = (H[i] >>> 8) & 0xff;
    out[4*i+3] = H[i] & 0xff;
  }
  return out;
}

// --- ECDH exports ---

// ScalarBaseMult computes k*G and returns the 65-byte uncompressed point.
export function ScalarBaseMult(scalar) {
  const k = os2ip(sliceToU8(scalar));
  return u8ToSlice(encodeUncompressed(scalarMult(k, G)));
}

// ScalarMult computes k*P and returns the 32-byte X coordinate (ECDH shared secret).
export function ScalarMult(scalar, point) {
  const k = os2ip(sliceToU8(scalar));
  const pt = decodeUncompressed(sliceToU8(point));
  if (!pt) return u8ToSlice(new Uint8Array(32));
  const r = scalarMult(k, pt);
  const aff = toAffine(r);
  if (!aff) return u8ToSlice(new Uint8Array(32));
  return u8ToSlice(i2osp(aff[0], 32));
}

// --- ECDSA exports ---
// Signatures are DER-encoded per MLS spec (RFC 9420 §5.1.2 signature_algorithm
// ecdsa_secp256r1_sha256 uses ASN.1 SEQUENCE{r,s}).

function derEncodeScalar(x) {
  const b = i2osp(x, 32);
  // Trim leading zeros
  let start = 0;
  while (start < 31 && b[start] === 0) start++;
  let trimmed = b.subarray(start);
  // If high bit set, prepend 0x00 to mark as positive.
  if (trimmed[0] & 0x80) {
    const out = new Uint8Array(trimmed.length + 1);
    out.set(trimmed, 1);
    trimmed = out;
  }
  const wrap = new Uint8Array(trimmed.length + 2);
  wrap[0] = 0x02;
  wrap[1] = trimmed.length;
  wrap.set(trimmed, 2);
  return wrap;
}

function derEncodeSig(r, s) {
  const rEnc = derEncodeScalar(r);
  const sEnc = derEncodeScalar(s);
  const inner = new Uint8Array(rEnc.length + sEnc.length);
  inner.set(rEnc);
  inner.set(sEnc, rEnc.length);
  const out = new Uint8Array(inner.length + 2);
  out[0] = 0x30;
  out[1] = inner.length;
  out.set(inner, 2);
  return out;
}

function derDecodeSig(b) {
  if (b.length < 8 || b[0] !== 0x30) return null;
  let off = 2;
  if (b[off++] !== 0x02) return null;
  let rLen = b[off++];
  const r = os2ip(b.subarray(off, off + rLen));
  off += rLen;
  if (b[off++] !== 0x02) return null;
  let sLen = b[off++];
  const s = os2ip(b.subarray(off, off + sLen));
  return [r, s];
}

// hashToScalar reduces SHA-256(msg) to a scalar mod N.
function hashToScalar(msg) {
  return os2ip(sha256(msg)) % N;
}

// Deterministic nonce per RFC 6979 §3.2 using HMAC-SHA-256.
function hmacSha256(key, data) {
  const bs = 64;
  let k = key;
  if (k.length > bs) k = sha256(k);
  if (k.length < bs) { const p = new Uint8Array(bs); p.set(k); k = p; }
  const ipad = new Uint8Array(bs);
  const opad = new Uint8Array(bs);
  for (let i = 0; i < bs; i++) { ipad[i] = k[i] ^ 0x36; opad[i] = k[i] ^ 0x5c; }
  const inner = new Uint8Array(bs + data.length);
  inner.set(ipad); inner.set(data, bs);
  const innerHash = sha256(inner);
  const outer = new Uint8Array(bs + 32);
  outer.set(opad); outer.set(innerHash, bs);
  return sha256(outer);
}

function rfc6979Nonce(x, hMsg) {
  // V = 0x01*32, K = 0x00*32
  let V = new Uint8Array(32).fill(0x01);
  let K = new Uint8Array(32);
  const xBytes = i2osp(x, 32);
  // K = HMAC_K(V || 0x00 || x || h)
  let buf = new Uint8Array(32 + 1 + 32 + 32);
  buf.set(V); buf[32] = 0x00; buf.set(xBytes, 33); buf.set(hMsg, 65);
  K = hmacSha256(K, buf);
  V = hmacSha256(K, V);
  buf = new Uint8Array(32 + 1 + 32 + 32);
  buf.set(V); buf[32] = 0x01; buf.set(xBytes, 33); buf.set(hMsg, 65);
  K = hmacSha256(K, buf);
  V = hmacSha256(K, V);
  while (true) {
    V = hmacSha256(K, V);
    const k = os2ip(V);
    if (k >= 1n && k < N) return k;
    const b = new Uint8Array(33);
    b.set(V); b[32] = 0x00;
    K = hmacSha256(K, b);
    V = hmacSha256(K, V);
  }
}

// Sign produces a DER-encoded ECDSA-P256-SHA256 signature.
// seed: 32-byte private key scalar (big-endian).
export function Sign(seed, message) {
  const d = os2ip(sliceToU8(seed)) % N;
  const msg = sliceToU8(message);
  const h = sha256(msg);
  const z = os2ip(h) % N;
  for (let iter = 0; iter < 32; iter++) {
    const k = rfc6979Nonce(d, h);
    const kG = scalarMult(k, G);
    const aff = toAffine(kG);
    if (!aff) continue;
    const r = aff[0] % N;
    if (r === 0n) continue;
    const kInv = modInv(k, N);
    const s = (kInv * ((z + r * d) % N)) % N;
    if (s === 0n) continue;
    return u8ToSlice(derEncodeSig(r, s));
  }
  return u8ToSlice(new Uint8Array(0));
}

// Verify validates a DER-encoded ECDSA-P256-SHA256 signature.
// pubkey: 65-byte uncompressed SEC1 (0x04 || X || Y).
export function Verify(pubkey, message, sig) {
  const pk = decodeUncompressed(sliceToU8(pubkey));
  if (!pk) return false;
  const msg = sliceToU8(message);
  const parsed = derDecodeSig(sliceToU8(sig));
  if (!parsed) return false;
  const [r, s] = parsed;
  if (r <= 0n || r >= N || s <= 0n || s >= N) return false;
  const z = os2ip(sha256(msg)) % N;
  const w = modInv(s, N);
  const u1 = (z * w) % N;
  const u2 = (r * w) % N;
  const p1 = scalarMult(u1, G);
  const p2 = scalarMult(u2, pk);
  const sum = jAdd(p1, p2);
  const aff = toAffine(sum);
  if (!aff) return false;
  return (aff[0] % N) === r;
}

// DeriveKeyPair per RFC 9180 §7.1.3 for DHKEM(P-256, HKDF-SHA256).
// Uses rejection sampling: sk = OS2IP(LabeledExpand(dkp_prk,"candidate",I2OSP(counter,1),32)) mod n.
// But the MLS caller does LabeledExpand — here we just expose a scalar reducer.
// ReduceScalar clamps x into [1, N-1] by mod, returning 32 big-endian bytes; callers must
// iterate with rejection if needed.
export function ReduceScalar(x) {
  const v = os2ip(sliceToU8(x));
  const r = v % N;
  if (r === 0n) return u8ToSlice(new Uint8Array(32));
  return u8ToSlice(i2osp(r, 32));
}

// ValidScalar returns true iff OS2IP(x) is in [1, N-1].
// Used by RFC 9180 DeriveKeyPair rejection sampling and signature key gen.
export function ValidScalar(x) {
  const v = os2ip(sliceToU8(x));
  return v >= 1n && v < N;
}
