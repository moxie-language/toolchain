// schnorr.mjs — BIP-340 Schnorr signatures using native JS BigInt.
// Bypasses moxie's uint64 emulation (which uses float64 and loses precision
// above 2^53). All 256-bit field/scalar arithmetic is exact via BigInt.

import { Slice } from './builtin.mjs';

const P  = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2Fn;
const N  = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141n;
const Gx = 0x79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798n;
const Gy = 0x483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8n;

// --- Modular arithmetic ---

function modP(x) { return ((x % P) + P) % P; }
function modN(x) { return ((x % N) + N) % N; }

function modInv(a, m) {
  let [old_r, r] = [((a % m) + m) % m, m];
  let [old_s, s] = [1n, 0n];
  while (r !== 0n) {
    const q = old_r / r;
    [old_r, r] = [r, old_r - q * r];
    [old_s, s] = [s, old_s - q * s];
  }
  return ((old_s % m) + m) % m;
}

// --- Jacobian point operations (X, Y, Z) → affine (X/Z², Y/Z³) ---

function jDouble(X1, Y1, Z1) {
  if (Y1 === 0n) return [0n, 1n, 0n];
  const A = modP(Y1 * Y1);
  const B = modP(4n * X1 * A);
  const C = modP(8n * A * A);
  const D = modP(3n * X1 * X1); // a=0 for secp256k1
  const X3 = modP(D * D - 2n * B);
  const Y3 = modP(D * (B - X3) - C);
  const Z3 = modP(2n * Y1 * Z1);
  return [X3, Y3, Z3];
}

function jAdd(X1, Y1, Z1, X2, Y2, Z2) {
  if (Z1 === 0n) return [X2, Y2, Z2];
  if (Z2 === 0n) return [X1, Y1, Z1];
  const Z1sq = modP(Z1 * Z1);
  const Z2sq = modP(Z2 * Z2);
  const U1 = modP(X1 * Z2sq);
  const U2 = modP(X2 * Z1sq);
  const S1 = modP(Y1 * Z2sq % P * Z2 % P);
  const S2 = modP(Y2 * Z1sq % P * Z1 % P);
  if (U1 === U2) {
    if (S1 === S2) return jDouble(X1, Y1, Z1);
    return [0n, 1n, 0n];
  }
  const H = modP(U2 - U1);
  const R = modP(S2 - S1);
  const H2 = modP(H * H);
  const H3 = modP(H2 * H);
  const X3 = modP(R * R - H3 - 2n * U1 * H2);
  const Y3 = modP(R * (U1 * H2 - X3) - S1 * H3);
  const Z3 = modP(H * Z1 * Z2);
  return [X3, Y3, Z3];
}

function jToAffine(X, Y, Z) {
  if (Z === 0n) return [0n, 0n];
  const Zi = modInv(Z, P);
  const Z2 = modP(Zi * Zi);
  const Z3 = modP(Z2 * Zi);
  return [modP(X * Z2), modP(Y * Z3)];
}

function scalarMult(k, px, py) {
  let [RX, RY, RZ] = [0n, 1n, 0n]; // infinity
  let [CX, CY, CZ] = [px, py, 1n];
  while (k > 0n) {
    if (k & 1n) [RX, RY, RZ] = jAdd(RX, RY, RZ, CX, CY, CZ);
    [CX, CY, CZ] = jDouble(CX, CY, CZ);
    k >>= 1n;
  }
  return jToAffine(RX, RY, RZ);
}

function liftX(x) {
  const c = modP(x * x * x + 7n);
  // y = c^((p+1)/4) mod p
  const exp = (P + 1n) / 4n;
  let y = modPow(c, exp, P);
  if (modP(y * y) !== modP(c)) return null;
  if (y % 2n !== 0n) y = modP(P - y);
  return y;
}

function modPow(base, exp, mod) {
  let result = 1n;
  base = ((base % mod) + mod) % mod;
  while (exp > 0n) {
    if (exp & 1n) result = result * base % mod;
    exp >>= 1n;
    base = base * base % mod;
  }
  return result;
}

// --- SHA-256 (synchronous, pure JS) ---

const K256 = [
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
];

function sha256(data) {
  // Padding.
  const len = data.length;
  const bitLen = len * 8;
  const padLen = ((56 - (len + 1) % 64) + 64) % 64;
  const padded = new Uint8Array(len + 1 + padLen + 8);
  padded.set(data);
  padded[len] = 0x80;
  // Big-endian 64-bit length (only lower 32 bits for messages < 2^32 bytes).
  const dv = new DataView(padded.buffer);
  dv.setUint32(padded.length - 4, bitLen, false);

  let [h0, h1, h2, h3, h4, h5, h6, h7] =
    [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];

  const w = new Int32Array(64);
  for (let off = 0; off < padded.length; off += 64) {
    for (let i = 0; i < 16; i++) w[i] = dv.getInt32(off + i * 4, false);
    for (let i = 16; i < 64; i++) {
      const s0 = (ror(w[i-15], 7) ^ ror(w[i-15], 18) ^ (w[i-15] >>> 3));
      const s1 = (ror(w[i-2], 17) ^ ror(w[i-2], 19) ^ (w[i-2] >>> 10));
      w[i] = (w[i-16] + s0 + w[i-7] + s1) | 0;
    }
    let [a, b, c, d, e, f, g, h] = [h0, h1, h2, h3, h4, h5, h6, h7];
    for (let i = 0; i < 64; i++) {
      const S1 = ror(e, 6) ^ ror(e, 11) ^ ror(e, 25);
      const ch = (e & f) ^ (~e & g);
      const t1 = (h + S1 + ch + K256[i] + w[i]) | 0;
      const S0 = ror(a, 2) ^ ror(a, 13) ^ ror(a, 22);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (S0 + maj) | 0;
      h = g; g = f; f = e; e = (d + t1) | 0;
      d = c; c = b; b = a; a = (t1 + t2) | 0;
    }
    h0 = (h0 + a) | 0; h1 = (h1 + b) | 0; h2 = (h2 + c) | 0; h3 = (h3 + d) | 0;
    h4 = (h4 + e) | 0; h5 = (h5 + f) | 0; h6 = (h6 + g) | 0; h7 = (h7 + h) | 0;
  }
  const out = new Uint8Array(32);
  const odv = new DataView(out.buffer);
  odv.setUint32(0, h0, false); odv.setUint32(4, h1, false);
  odv.setUint32(8, h2, false); odv.setUint32(12, h3, false);
  odv.setUint32(16, h4, false); odv.setUint32(20, h5, false);
  odv.setUint32(24, h6, false); odv.setUint32(28, h7, false);
  return out;
}

function ror(x, n) { return ((x >>> n) | (x << (32 - n))) | 0; }

function taggedHash(tag, msg) {
  const tagH = sha256(new TextEncoder().encode(tag));
  const buf = new Uint8Array(64 + msg.length);
  buf.set(tagH, 0);
  buf.set(tagH, 32);
  buf.set(msg, 64);
  return sha256(buf);
}

// --- Conversions between Go slices and BigInt/Uint8Array ---

function sliceToU8(s) {
  if (s instanceof Uint8Array) return s;
  const u = new Uint8Array(s.$length);
  for (let i = 0; i < s.$length; i++) u[i] = s.$array[s.$offset + i];
  return u;
}

function u8ToSlice(u8) {
  const arr = new Array(u8.length);
  for (let i = 0; i < u8.length; i++) arr[i] = u8[i];
  return new Slice(arr, 0, u8.length, u8.length);
}

function bytesToBigInt(u8) {
  let n = 0n;
  for (let i = 0; i < u8.length; i++) n = (n << 8n) | BigInt(u8[i]);
  return n;
}

function bigIntTo32(n) {
  const out = new Uint8Array(32);
  for (let i = 31; i >= 0; i--) { out[i] = Number(n & 0xFFn); n >>= 8n; }
  return out;
}

// --- Exported BIP-340 functions (called from Go via jsbridge) ---

// PubKeyFromSecKey computes the x-only public key.
// Returns the pubkey as a Go slice, and a bool.
export function PubKeyFromSecKey(seckey) {
  const sk = bytesToBigInt(sliceToU8(seckey));
  if (sk === 0n || sk >= N) return [u8ToSlice(new Uint8Array(32)), false];
  const [px, _] = scalarMult(sk, Gx, Gy);
  return [u8ToSlice(bigIntTo32(px)), true];
}

// SignSchnorr signs a message using BIP-340.
// seckey, msg, auxRand are Go slices of 32 bytes each.
// Returns [sigSlice (64 bytes), bool].
export function SignSchnorr(seckey, msg, auxRand) {
  const sk = bytesToBigInt(sliceToU8(seckey));
  const m = sliceToU8(msg);
  const aux = sliceToU8(auxRand);

  if (sk === 0n || sk >= N) return [u8ToSlice(new Uint8Array(64)), false];

  // P = sk * G
  const [px, py] = scalarMult(sk, Gx, Gy);

  // If P.y is odd, negate sk.
  let d = sk;
  if (py % 2n !== 0n) d = N - d;

  const pkBytes = bigIntTo32(px);

  // t = d XOR tagged_hash("BIP0340/aux", auxRand)
  const auxHash = taggedHash("BIP0340/aux", aux);
  const dBytes = bigIntTo32(d);
  const t = new Uint8Array(32);
  for (let i = 0; i < 32; i++) t[i] = dBytes[i] ^ auxHash[i];

  // k' = tagged_hash("BIP0340/nonce", t || pk || msg) mod n
  const nonceInput = new Uint8Array(96);
  nonceInput.set(t, 0);
  nonceInput.set(pkBytes, 32);
  nonceInput.set(m, 64);
  const kHash = taggedHash("BIP0340/nonce", nonceInput);
  let k = bytesToBigInt(kHash) % N;
  if (k === 0n) return [u8ToSlice(new Uint8Array(64)), false];

  // R = k * G
  const [rx, ry] = scalarMult(k, Gx, Gy);

  // If R.y is odd, negate k.
  if (ry % 2n !== 0n) k = N - k;

  const rxBytes = bigIntTo32(rx);

  // e = tagged_hash("BIP0340/challenge", R.x || P.x || msg) mod n
  const eInput = new Uint8Array(96);
  eInput.set(rxBytes, 0);
  eInput.set(pkBytes, 32);
  eInput.set(m, 64);
  const eHash = taggedHash("BIP0340/challenge", eInput);
  const e = bytesToBigInt(eHash) % N;

  // sig = R.x || (k + e*d) mod n
  const s = modN(k + e * d);

  const sig = new Uint8Array(64);
  sig.set(rxBytes, 0);
  sig.set(bigIntTo32(s), 32);

  return [u8ToSlice(sig), true];
}

// VerifySchnorr verifies a BIP-340 signature.
export function VerifySchnorr(pubkey, msg, sig) {
  const pkU8 = sliceToU8(pubkey);
  const mU8 = sliceToU8(msg);
  const sigU8 = sliceToU8(sig);

  const px = bytesToBigInt(pkU8);
  const py = liftX(px);
  if (py === null) return false;

  const rx = bytesToBigInt(sigU8.slice(0, 32));
  const s = bytesToBigInt(sigU8.slice(32, 64));
  if (rx >= P || s >= N) return false;

  // e = tagged_hash("BIP0340/challenge", R.x || P.x || msg) mod n
  const eInput = new Uint8Array(96);
  eInput.set(sigU8.slice(0, 32), 0);
  eInput.set(pkU8, 32);
  eInput.set(mU8, 64);
  const eHash = taggedHash("BIP0340/challenge", eInput);
  const e = bytesToBigInt(eHash) % N;

  // R = s*G - e*P
  const [sGx, sGy] = scalarMult(s, Gx, Gy);
  const [ePx, ePy] = scalarMult(e, px, py);
  // s*G + (-e)*P
  const negEPy = modP(P - ePy);
  const [Rx, Ry] = affineAdd(sGx, sGy, ePx, negEPy);

  if (Rx === 0n && Ry === 0n) return false;
  if (Ry % 2n !== 0n) return false;
  if (Rx !== rx) return false;
  return true;
}

function affineAdd(x1, y1, x2, y2) {
  if (x1 === 0n && y1 === 0n) return [x2, y2];
  if (x2 === 0n && y2 === 0n) return [x1, y1];
  if (x1 === x2) {
    if (y1 === y2) return affineDbl(x1, y1);
    return [0n, 0n];
  }
  const lam = modP((y2 - y1) * modInv(x2 - x1, P));
  const x3 = modP(lam * lam - x1 - x2);
  const y3 = modP(lam * (x1 - x3) - y1);
  return [x3, y3];
}

function affineDbl(x1, y1) {
  if (y1 === 0n) return [0n, 0n];
  const lam = modP(3n * x1 * x1 * modInv(2n * y1, P));
  const x3 = modP(lam * lam - 2n * x1);
  const y3 = modP(lam * (x1 - x3) - y1);
  return [x3, y3];
}

// ECDH computes the shared secret x-coordinate.
export function ECDH(seckey, pubkey) {
  const sk = bytesToBigInt(sliceToU8(seckey));
  const px = bytesToBigInt(sliceToU8(pubkey));
  if (sk === 0n || sk >= N) return [u8ToSlice(new Uint8Array(32)), false];
  const py = liftX(px);
  if (py === null) return [u8ToSlice(new Uint8Array(32)), false];
  const [rx, _] = scalarMult(sk, px, py);
  if (rx === 0n) return [u8ToSlice(new Uint8Array(32)), false];
  return [u8ToSlice(bigIntTo32(rx)), true];
}
