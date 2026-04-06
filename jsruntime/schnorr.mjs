// schnorr.mjs — BIP-340 Schnorr signatures using JS BigInt.
// Field/scalar arithmetic over secp256k1 with wNAF precomputed tables
// and Shamir's trick for fast verification.

import { Slice } from './builtin.mjs';

const P  = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2Fn;
const N  = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141n;
const Gx = 0x79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798n;
const Gy = 0x483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8n;

// --- Modular arithmetic ---

function mod(x, m) { return ((x % m) + m) % m; }

function modInv(a, m) {
  let [old_r, r] = [mod(a, m), m];
  let [old_s, s] = [1n, 0n];
  while (r !== 0n) {
    const q = old_r / r;
    [old_r, r] = [r, old_r - q * r];
    [old_s, s] = [s, old_s - q * s];
  }
  return mod(old_s, m);
}

function modPow(base, exp, m) {
  let result = 1n;
  base = mod(base, m);
  while (exp > 0n) {
    if (exp & 1n) result = result * base % m;
    exp >>= 1n;
    base = base * base % m;
  }
  return result;
}

// --- Jacobian point: [X, Y, Z] where affine x=X/Z², y=Y/Z³ ---
// Point at infinity: Z === 0n.

function jDbl(X1, Y1, Z1) {
  if (Y1 === 0n) return [0n, 1n, 0n];
  const A = mod(Y1 * Y1, P);
  const B = mod(4n * X1 * A, P);
  const C = mod(8n * A * A, P);
  const D = mod(3n * X1 * X1, P); // a=0 for secp256k1
  const X3 = mod(D * D - 2n * B, P);
  const Y3 = mod(D * (B - X3) - C, P);
  const Z3 = mod(2n * Y1 * Z1, P);
  return [X3, Y3, Z3];
}

function jAdd(X1, Y1, Z1, X2, Y2, Z2) {
  if (Z1 === 0n) return [X2, Y2, Z2];
  if (Z2 === 0n) return [X1, Y1, Z1];
  const Z1sq = mod(Z1 * Z1, P);
  const Z2sq = mod(Z2 * Z2, P);
  const U1 = mod(X1 * Z2sq, P);
  const U2 = mod(X2 * Z1sq, P);
  const S1 = mod(Y1 * mod(Z2sq * Z2, P), P);
  const S2 = mod(Y2 * mod(Z1sq * Z1, P), P);
  if (U1 === U2) {
    if (S1 === S2) return jDbl(X1, Y1, Z1);
    return [0n, 1n, 0n];
  }
  const H = mod(U2 - U1, P);
  const R = mod(S2 - S1, P);
  const H2 = mod(H * H, P);
  const H3 = mod(H2 * H, P);
  const X3 = mod(R * R - H3 - 2n * U1 * H2, P);
  const Y3 = mod(R * (U1 * H2 - X3) - S1 * H3, P);
  const Z3 = mod(H * Z1 * Z2, P);
  return [X3, Y3, Z3];
}

function jNeg(X, Y, Z) {
  return [X, mod(P - Y, P), Z];
}

function jToAffine(X, Y, Z) {
  if (Z === 0n) return [0n, 0n];
  const Zi = modInv(Z, P);
  const Z2 = mod(Zi * Zi, P);
  const Z3 = mod(Z2 * Zi, P);
  return [mod(X * Z2, P), mod(Y * Z3, P)];
}

// --- wNAF precomputed table for generator G ---
// Window size W=8: precompute odd multiples 1G, 3G, 5G, ..., 255G.
// Then scalarMultG processes 8 bits at a time using table lookup.

const W = 8;
const HALF = 1 << (W - 1); // 128
const MASK = (1 << W) - 1; // 255

// Precomputed table: _gTable[i] = (2i+1)*G in Jacobian [X, Y, Z].
// Negation (for negative wNAF digits) is free: just negate Y.
let _gTable = null;

function ensureGTable() {
  if (_gTable) return;
  _gTable = new Array(HALF);
  // 1*G
  _gTable[0] = [Gx, Gy, 1n];
  // 2*G
  const g2 = jDbl(Gx, Gy, 1n);
  for (let i = 1; i < HALF; i++) {
    _gTable[i] = jAdd(_gTable[i - 1][0], _gTable[i - 1][1], _gTable[i - 1][2],
                       g2[0], g2[1], g2[2]);
  }
}

// Encode scalar k into wNAF form with window W.
// Returns array of digits in {-(2^(W-1)-1) .. 2^(W-1)-1}, odd or zero.
function wnaf(k, w) {
  const digits = [];
  const halfW = 1n << BigInt(w - 1);
  const fullW = halfW << 1n;
  const mask = fullW - 1n;
  while (k > 0n) {
    if (k & 1n) {
      let d = k & mask;
      if (d >= halfW) d -= fullW;
      k -= d;
      digits.push(d);
    } else {
      digits.push(0n);
    }
    k >>= 1n;
  }
  return digits;
}

// scalarMultG: fast k*G using wNAF precomputed table.
function scalarMultG(k) {
  ensureGTable();
  k = ((k % N) + N) % N;
  if (k === 0n) return [0n, 0n];

  const digits = wnaf(k, W);
  let RX = 0n, RY = 1n, RZ = 0n; // infinity

  for (let i = digits.length - 1; i >= 0; i--) {
    [RX, RY, RZ] = jDbl(RX, RY, RZ);
    const d = digits[i];
    if (d !== 0n) {
      const idx = Number(d < 0n ? (-d - 1n) >> 1n : (d - 1n) >> 1n);
      const pt = _gTable[idx];
      if (d < 0n) {
        [RX, RY, RZ] = jAdd(RX, RY, RZ, pt[0], mod(P - pt[1], P), pt[2]);
      } else {
        [RX, RY, RZ] = jAdd(RX, RY, RZ, pt[0], pt[1], pt[2]);
      }
    }
  }
  return jToAffine(RX, RY, RZ);
}

// Plain scalar multiply for arbitrary points (no precomputation).
function scalarMult(k, px, py) {
  let [RX, RY, RZ] = [0n, 1n, 0n];
  let [CX, CY, CZ] = [px, py, 1n];
  k = ((k % N) + N) % N;
  while (k > 0n) {
    if (k & 1n) [RX, RY, RZ] = jAdd(RX, RY, RZ, CX, CY, CZ);
    [CX, CY, CZ] = jDbl(CX, CY, CZ);
    k >>= 1n;
  }
  return jToAffine(RX, RY, RZ);
}

// Shamir's trick: compute k1*G + k2*P in one pass.
// Shares doublings between the two scalar multiplications.
function shamirMultGP(k1, k2, px, py) {
  ensureGTable();
  k1 = ((k1 % N) + N) % N;
  k2 = ((k2 % N) + N) % N;

  // wNAF encode k1 (for G table lookup)
  const d1 = wnaf(k1, W);
  // Simple binary for k2 (no precomputed table for P)
  const d2 = wnaf(k2, 2);

  const len = Math.max(d1.length, d2.length);

  let RX = 0n, RY = 1n, RZ = 0n; // infinity

  for (let i = len - 1; i >= 0; i--) {
    [RX, RY, RZ] = jDbl(RX, RY, RZ);

    // k1*G part: use precomputed table
    const a = i < d1.length ? d1[i] : 0n;
    if (a !== 0n) {
      const idx = Number(a < 0n ? (-a - 1n) >> 1n : (a - 1n) >> 1n);
      const pt = _gTable[idx];
      if (a < 0n) {
        [RX, RY, RZ] = jAdd(RX, RY, RZ, pt[0], mod(P - pt[1], P), pt[2]);
      } else {
        [RX, RY, RZ] = jAdd(RX, RY, RZ, pt[0], pt[1], pt[2]);
      }
    }

    // k2*P part: simple binary (odd digit = ±1 only)
    const b = i < d2.length ? d2[i] : 0n;
    if (b > 0n) {
      [RX, RY, RZ] = jAdd(RX, RY, RZ, px, py, 1n);
    } else if (b < 0n) {
      [RX, RY, RZ] = jAdd(RX, RY, RZ, px, mod(P - py, P), 1n);
    }
  }

  return jToAffine(RX, RY, RZ);
}

function liftX(x) {
  const c = mod(x * x * x + 7n, P);
  const exp = (P + 1n) / 4n;
  let y = modPow(c, exp, P);
  if (mod(y * y, P) !== mod(c, P)) return null;
  if (y % 2n !== 0n) y = mod(P - y, P);
  return y;
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
  const len = data.length;
  const bitLen = len * 8;
  const padLen = ((56 - (len + 1) % 64) + 64) % 64;
  const padded = new Uint8Array(len + 1 + padLen + 8);
  padded.set(data);
  padded[len] = 0x80;
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

// --- Slice conversions ---

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

// --- Exported BIP-340 functions ---

export function PubKeyFromSecKey(seckey) {
  const sk = bytesToBigInt(sliceToU8(seckey));
  if (sk === 0n || sk >= N) return [u8ToSlice(new Uint8Array(32)), false];
  const [px, _] = scalarMultG(sk);
  return [u8ToSlice(bigIntTo32(px)), true];
}

export function SignSchnorr(seckey, msg, auxRand) {
  const sk = bytesToBigInt(sliceToU8(seckey));
  const m = sliceToU8(msg);
  const aux = sliceToU8(auxRand);

  if (sk === 0n || sk >= N) return [u8ToSlice(new Uint8Array(64)), false];

  const [px, py] = scalarMultG(sk);
  let d = sk;
  if (py % 2n !== 0n) d = N - d;

  const pkBytes = bigIntTo32(px);

  const auxHash = taggedHash("BIP0340/aux", aux);
  const dBytes = bigIntTo32(d);
  const t = new Uint8Array(32);
  for (let i = 0; i < 32; i++) t[i] = dBytes[i] ^ auxHash[i];

  const nonceInput = new Uint8Array(96);
  nonceInput.set(t, 0);
  nonceInput.set(pkBytes, 32);
  nonceInput.set(m, 64);
  const kHash = taggedHash("BIP0340/nonce", nonceInput);
  let k = bytesToBigInt(kHash) % N;
  if (k === 0n) return [u8ToSlice(new Uint8Array(64)), false];

  const [rx, ry] = scalarMultG(k);
  if (ry % 2n !== 0n) k = N - k;

  const rxBytes = bigIntTo32(rx);

  const eInput = new Uint8Array(96);
  eInput.set(rxBytes, 0);
  eInput.set(pkBytes, 32);
  eInput.set(m, 64);
  const eHash = taggedHash("BIP0340/challenge", eInput);
  const e = bytesToBigInt(eHash) % N;

  const s = ((k + e * d) % N + N) % N;

  const sig = new Uint8Array(64);
  sig.set(rxBytes, 0);
  sig.set(bigIntTo32(s), 32);

  return [u8ToSlice(sig), true];
}

// VerifySchnorr — uses Shamir's trick for s*G + (-e)*P.
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

  const eInput = new Uint8Array(96);
  eInput.set(sigU8.slice(0, 32), 0);
  eInput.set(pkU8, 32);
  eInput.set(mU8, 64);
  const eHash = taggedHash("BIP0340/challenge", eInput);
  const e = bytesToBigInt(eHash) % N;

  // R = s*G + (-e)*P via Shamir's trick.
  const negE = ((N - e) % N + N) % N;
  const [Rx, Ry] = shamirMultGP(s, negE, px, py);

  if (Rx === 0n && Ry === 0n) return false;
  if (Ry % 2n !== 0n) return false;
  if (Rx !== rx) return false;
  return true;
}

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

export function SHA256Sum(data) {
  return u8ToSlice(sha256(sliceToU8(data)));
}

export function ScalarAddModN(a, b) {
  const ai = bytesToBigInt(sliceToU8(a));
  const bi = bytesToBigInt(sliceToU8(b));
  const sum = ((ai + bi) % N + N) % N;
  if (sum === 0n) return [u8ToSlice(new Uint8Array(32)), false];
  return [u8ToSlice(bigIntTo32(sum)), true];
}

export function CompressedPubKey(seckey) {
  const sk = bytesToBigInt(sliceToU8(seckey));
  if (sk === 0n || sk >= N) return [u8ToSlice(new Uint8Array(33)), false];
  const [px, py] = scalarMultG(sk);
  const out = new Uint8Array(33);
  out[0] = (py % 2n === 0n) ? 0x02 : 0x03;
  out.set(bigIntTo32(px), 1);
  return [u8ToSlice(out), true];
}
