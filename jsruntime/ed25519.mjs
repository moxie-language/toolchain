// ed25519.mjs — Ed25519 signatures (RFC 8032) using JS BigInt.
// Twisted Edwards curve: -x² + y² = 1 + d·x²·y² over GF(2²⁵⁵ − 19).

import { Slice } from './builtin.mjs';

// --- Constants ---

const P = (1n << 255n) - 19n;
const L = (1n << 252n) + 27742317777372353535851937790883648493n;
const D = 37095705934669439343138083508754565189542113879843219016388785533085940283555n;
const I = 19681161376707505956807079304988542015446066515923890162744021073123829784752n; // sqrt(-1) mod p
const Bx = 15112221349535400772501151409588531511454012693041857206046113283949847762202n;
const By = 46316835694926478169428394003475163141307993866256225615783033603165251855960n;

// --- Field arithmetic mod P ---

function modP(x) { return ((x % P) + P) % P; }

function modPow(base, exp) {
  let r = 1n;
  base = modP(base);
  while (exp > 0n) {
    if (exp & 1n) r = r * base % P;
    exp >>= 1n;
    base = base * base % P;
  }
  return r;
}

function modInv(a) { return modPow(a, P - 2n); }

// --- Extended twisted Edwards point arithmetic ---
// Representation: [X, Y, Z, T] where x=X/Z, y=Y/Z, T=X·Y/Z.
// Identity: [0, 1, 1, 0].

function extDbl(p) {
  const [X, Y, Z] = p;
  const A = modP(X * X);
  const B = modP(Y * Y);
  const C = modP(2n * Z * Z);
  // a = -1: D = a*A = -A. EFD dbl-2008-hwcd formulas.
  const E = modP(modP((X + Y) * (X + Y)) - A - B);
  const G = modP(B - A);       // D + B where D = -A
  const F = modP(G - C);       // G - C
  const H = modP(-A - B);      // D - B where D = -A
  return [modP(E * F), modP(G * H), modP(F * G), modP(E * H)];
}

function extAdd(p1, p2) {
  const [X1, Y1, Z1, T1] = p1;
  const [X2, Y2, Z2, T2] = p2;
  const A = modP((Y1 - X1) * (Y2 - X2));
  const B = modP((Y1 + X1) * (Y2 + X2));
  const C = modP(T1 * 2n * D * T2);
  const E = modP(B - A);
  const DD = modP(Z1 * 2n * Z2);
  const F = modP(DD - C);
  const G = modP(DD + C);
  const H = modP(B + A);
  return [modP(E * F), modP(G * H), modP(F * G), modP(E * H)];
}

function scalarMult(k, p) {
  let r = [0n, 1n, 1n, 0n];
  let q = p;
  k = ((k % L) + L) % L;
  while (k > 0n) {
    if (k & 1n) r = extAdd(r, q);
    q = extDbl(q);
    k >>= 1n;
  }
  return r;
}

const B = [Bx, By, 1n, modP(Bx * By)];

function scalarMultBase(k) { return scalarMult(k, B); }

// --- Point encoding / decoding ---

function encodePoint(p) {
  const zi = modInv(p[2]);
  const x = modP(p[0] * zi);
  const y = modP(p[1] * zi);
  const out = new Uint8Array(32);
  let yy = y;
  for (let i = 0; i < 32; i++) { out[i] = Number(yy & 0xFFn); yy >>= 8n; }
  if (x & 1n) out[31] |= 0x80;
  return out;
}

function decodePoint(bytes) {
  let y = 0n;
  for (let i = 31; i >= 0; i--) y = (y << 8n) | BigInt(bytes[i]);
  const sign = (y >> 255n) & 1n;
  y &= (1n << 255n) - 1n;
  if (y >= P) return null;

  const y2 = modP(y * y);
  const u = modP(y2 - 1n);
  const v = modP(D * y2 + 1n);

  // x = u·v³ · (u·v⁷)^((p-5)/8)
  const v3 = modP(v * v * v);
  const uv3 = modP(u * v3);
  const uv7 = modP(uv3 * modP(v3 * v));
  let x = modP(uv3 * modPow(uv7, (P - 5n) / 8n));

  if (modP(x * x * v - u) !== 0n) {
    x = modP(x * I);
    if (modP(x * x * v - u) !== 0n) return null;
  }

  if ((x & 1n) !== sign) x = modP(P - x);
  if (x === 0n && sign) return null;

  return [x, y, 1n, modP(x * y)];
}

// --- SHA-512 (inline, BigInt-based for 64-bit words) ---

const K512 = [
  0x428a2f98d728ae22n, 0x7137449123ef65cdn, 0xb5c0fbcfec4d3b2fn, 0xe9b5dba58189dbbcn,
  0x3956c25bf348b538n, 0x59f111f1b605d019n, 0x923f82a4af194f9bn, 0xab1c5ed5da6d8118n,
  0xd807aa98a3030242n, 0x12835b0145706fben, 0x243185be4ee4b28cn, 0x550c7dc3d5ffb4e2n,
  0x72be5d74f27b896fn, 0x80deb1fe3b1696b1n, 0x9bdc06a725c71235n, 0xc19bf174cf692694n,
  0xe49b69c19ef14ad2n, 0xefbe4786384f25e3n, 0x0fc19dc68b8cd5b5n, 0x240ca1cc77ac9c65n,
  0x2de92c6f592b0275n, 0x4a7484aa6ea6e483n, 0x5cb0a9dcbd41fbd4n, 0x76f988da831153b5n,
  0x983e5152ee66dfabn, 0xa831c66d2db43210n, 0xb00327c898fb213fn, 0xbf597fc7beef0ee4n,
  0xc6e00bf33da88fc2n, 0xd5a79147930aa725n, 0x06ca6351e003826fn, 0x142929670a0e6e70n,
  0x27b70a8546d22ffcn, 0x2e1b21385c26c926n, 0x4d2c6dfc5ac42aedn, 0x53380d139d95b3dfn,
  0x650a73548baf63den, 0x766a0abb3c77b2a8n, 0x81c2c92e47edaee6n, 0x92722c851482353bn,
  0xa2bfe8a14cf10364n, 0xa81a664bbc423001n, 0xc24b8b70d0f89791n, 0xc76c51a30654be30n,
  0xd192e819d6ef5218n, 0xd69906245565a910n, 0xf40e35855771202an, 0x106aa07032bbd1b8n,
  0x19a4c116b8d2d0c8n, 0x1e376c085141ab53n, 0x2748774cdf8eeb99n, 0x34b0bcb5e19b48a8n,
  0x391c0cb3c5c95a63n, 0x4ed8aa4ae3418acbn, 0x5b9cca4f7763e373n, 0x682e6ff3d6b2b8a3n,
  0x748f82ee5defb2fcn, 0x78a5636f43172f60n, 0x84c87814a1f0ab72n, 0x8cc702081a6439ecn,
  0x90befffa23631e28n, 0xa4506cebde82bde9n, 0xbef9a3f7b2c67915n, 0xc67178f2e372532bn,
  0xca273eceea26619cn, 0xd186b8c721c0c207n, 0xeada7dd6cde0eb1en, 0xf57d4f7fee6ed178n,
  0x06f067aa72176fban, 0x0a637dc5a2c898a6n, 0x113f9804bef90daen, 0x1b710b35131c471bn,
  0x28db77f523047d84n, 0x32caab7b40c72493n, 0x3c9ebe0a15c9bebcn, 0x431d67c49c100d4cn,
  0x4cc5d4becb3e42b6n, 0x597f299cfc657e2an, 0x5fcb6fab3ad6faecn, 0x6c44198c4a475817n,
];
const M64 = 0xFFFFFFFFFFFFFFFFn;
function rotr64(x, n) { const b = BigInt(n); return ((x >> b) | (x << (64n - b))) & M64; }

function sha512(data) {
  const len = data.length;
  const bitLen = BigInt(len) * 8n;
  const padLen = ((112 - (len + 1) % 128) + 128) % 128;
  const buf = new Uint8Array(len + 1 + padLen + 16);
  buf.set(data);
  buf[len] = 0x80;
  const dv = new DataView(buf.buffer);
  dv.setUint32(buf.length - 8, Number((bitLen >> 32n) & 0xFFFFFFFFn), false);
  dv.setUint32(buf.length - 4, Number(bitLen & 0xFFFFFFFFn), false);

  let h0=0x6a09e667f3bcc908n, h1=0xbb67ae8584caa73bn, h2=0x3c6ef372fe94f82bn, h3=0xa54ff53a5f1d36f1n;
  let h4=0x510e527fade682d1n, h5=0x9b05688c2b3e6c1fn, h6=0x1f83d9abfb41bd6bn, h7=0x5be0cd19137e2179n;

  function rd64(o) { return (BigInt(dv.getUint32(o, false)) << 32n) | BigInt(dv.getUint32(o+4, false)); }
  const W = new Array(80);
  for (let off = 0; off < buf.length; off += 128) {
    for (let i = 0; i < 16; i++) W[i] = rd64(off + i * 8);
    for (let i = 16; i < 80; i++) {
      const s0 = rotr64(W[i-15],1) ^ rotr64(W[i-15],8) ^ (W[i-15] >> 7n);
      const s1 = rotr64(W[i-2],19) ^ rotr64(W[i-2],61) ^ (W[i-2] >> 6n);
      W[i] = (W[i-16] + s0 + W[i-7] + s1) & M64;
    }
    let a=h0, b=h1, c=h2, d=h3, e=h4, f=h5, g=h6, h=h7;
    for (let i = 0; i < 80; i++) {
      const S1 = rotr64(e,14) ^ rotr64(e,18) ^ rotr64(e,41);
      const ch = (e & f) ^ ((~e & M64) & g);
      const t1 = (h + S1 + ch + K512[i] + W[i]) & M64;
      const S0 = rotr64(a,28) ^ rotr64(a,34) ^ rotr64(a,39);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (S0 + maj) & M64;
      h=g; g=f; f=e; e=(d+t1)&M64; d=c; c=b; b=a; a=(t1+t2)&M64;
    }
    h0=(h0+a)&M64; h1=(h1+b)&M64; h2=(h2+c)&M64; h3=(h3+d)&M64;
    h4=(h4+e)&M64; h5=(h5+f)&M64; h6=(h6+g)&M64; h7=(h7+h)&M64;
  }

  const out = new Uint8Array(64);
  const ov = new DataView(out.buffer);
  function wr64(o, v) { ov.setUint32(o, Number(v >> 32n), false); ov.setUint32(o+4, Number(v & 0xFFFFFFFFn), false); }
  wr64(0,h0); wr64(8,h1); wr64(16,h2); wr64(24,h3);
  wr64(32,h4); wr64(40,h5); wr64(48,h6); wr64(56,h7);
  return out;
}

// --- Scalar helpers ---

function clampScalar(h) {
  const a = new Uint8Array(32);
  for (let i = 0; i < 32; i++) a[i] = h[i];
  a[0] &= 248;
  a[31] &= 127;
  a[31] |= 64;
  let s = 0n;
  for (let i = 31; i >= 0; i--) s = (s << 8n) | BigInt(a[i]);
  return s;
}

function bytesToScalarLE(h) {
  let s = 0n;
  for (let i = h.length - 1; i >= 0; i--) s = (s << 8n) | BigInt(h[i]);
  return ((s % L) + L) % L;
}

function scalarToBytes(s) {
  const out = new Uint8Array(32);
  for (let i = 0; i < 32; i++) { out[i] = Number(s & 0xFFn); s >>= 8n; }
  return out;
}

// --- Slice conversions ---

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

// --- Exports ---

// NewKeyFromSeed derives the 32-byte public key from a 32-byte seed.
export function NewKeyFromSeed(seed) {
  const s = sliceToU8(seed);
  const h = sha512(s);
  const a = clampScalar(h);
  return u8ToSlice(encodePoint(scalarMultBase(a)));
}

// Sign produces a 64-byte Ed25519 signature of message using a 32-byte seed.
export function Sign(seed, message) {
  const s = sliceToU8(seed);
  const msg = sliceToU8(message);
  const h = sha512(s);
  const a = clampScalar(h);
  const A = encodePoint(scalarMultBase(a));

  // r = SHA-512(h[32:64] || msg) mod L
  const rBuf = new Uint8Array(32 + msg.length);
  for (let i = 0; i < 32; i++) rBuf[i] = h[32 + i];
  rBuf.set(msg, 32);
  const r = bytesToScalarLE(sha512(rBuf));

  // R = r·B
  const R = encodePoint(scalarMultBase(r));

  // S = (r + SHA-512(R || A || msg) · a) mod L
  const hramBuf = new Uint8Array(32 + 32 + msg.length);
  hramBuf.set(R);
  hramBuf.set(A, 32);
  hramBuf.set(msg, 64);
  const hram = bytesToScalarLE(sha512(hramBuf));
  const S = ((r + hram * a) % L + L) % L;

  const sig = new Uint8Array(64);
  sig.set(R);
  sig.set(scalarToBytes(S), 32);
  return u8ToSlice(sig);
}

// Verify checks a 64-byte Ed25519 signature of message against a 32-byte public key.
export function Verify(pubkey, message, sig) {
  const pkBytes = sliceToU8(pubkey);
  const msg = sliceToU8(message);
  const sigBytes = sliceToU8(sig);
  if (sigBytes.length !== 64 || pkBytes.length !== 32) return false;

  const A = decodePoint(pkBytes);
  if (!A) return false;
  const Rpt = decodePoint(sigBytes.slice(0, 32));
  if (!Rpt) return false;

  let S = 0n;
  for (let i = 31; i >= 0; i--) S = (S << 8n) | BigInt(sigBytes[32 + i]);
  if (S >= L) return false;

  // h = SHA-512(R || A || msg) mod L
  const hBuf = new Uint8Array(32 + 32 + msg.length);
  hBuf.set(sigBytes.slice(0, 32));
  hBuf.set(pkBytes, 32);
  hBuf.set(msg, 64);
  const h = bytesToScalarLE(sha512(hBuf));

  // Check: S·B = R + h·A
  const lhs = scalarMultBase(S);
  const rhs = extAdd(Rpt, scalarMult(h, A));

  const lhsEnc = encodePoint(lhs);
  const rhsEnc = encodePoint(rhs);
  for (let i = 0; i < 32; i++) if (lhsEnc[i] !== rhsEnc[i]) return false;
  return true;
}
