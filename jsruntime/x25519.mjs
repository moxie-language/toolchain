// x25519.mjs — X25519 Diffie-Hellman (RFC 7748) using JS BigInt.
// Montgomery ladder over Curve25519: y² = x³ + 486662x² + x (mod 2²⁵⁵ − 19).

import { Slice } from './builtin.mjs';

const P   = (1n << 255n) - 19n;
const A24 = 121665n; // (486662 - 2) / 4

function mod(x) { return ((x % P) + P) % P; }

function modPow(base, exp) {
  let r = 1n;
  base = mod(base);
  while (exp > 0n) {
    if (exp & 1n) r = r * base % P;
    exp >>= 1n;
    base = base * base % P;
  }
  return r;
}

function modInv(a) { return modPow(a, P - 2n); }

// RFC 7748 §5: Montgomery ladder, constant-time in the bit-scan pattern.
function ladder(k, u) {
  let x_2 = 1n, z_2 = 0n;
  let x_3 = u,  z_3 = 1n;
  let swap = 0n;

  for (let t = 254; t >= 0; t--) {
    const k_t = (k >> BigInt(t)) & 1n;
    swap ^= k_t;
    if (swap) { [x_2, x_3] = [x_3, x_2]; [z_2, z_3] = [z_3, z_2]; }
    swap = k_t;

    const A  = mod(x_2 + z_2);
    const AA = mod(A * A);
    const B  = mod(x_2 - z_2);
    const BB = mod(B * B);
    const E  = mod(AA - BB);
    const C  = mod(x_3 + z_3);
    const D  = mod(x_3 - z_3);
    const DA = mod(D * A);
    const CB = mod(C * B);
    x_3 = mod((DA + CB) * (DA + CB));
    z_3 = mod(u * mod((DA - CB) * (DA - CB)));
    x_2 = mod(AA * BB);
    z_2 = mod(E * (AA + A24 * E));
  }

  if (swap) { [x_2, x_3] = [x_3, x_2]; [z_2, z_3] = [z_3, z_2]; }
  return mod(x_2 * modInv(z_2));
}

// RFC 7748 §5: decode scalar, clamp, decode u-coordinate.
function decodeScalar(u8) {
  const s = new Uint8Array(u8);
  s[0]  &= 248;
  s[31] &= 127;
  s[31] |= 64;
  let n = 0n;
  for (let i = 31; i >= 0; i--) n = (n << 8n) | BigInt(s[i]);
  return n;
}

function decodeU(u8) {
  // Mask bit 255 per RFC 7748.
  let n = 0n;
  for (let i = 31; i >= 0; i--) n = (n << 8n) | BigInt(u8[i]);
  return n & ((1n << 255n) - 1n);
}

function encodeU(n) {
  const out = new Uint8Array(32);
  for (let i = 0; i < 32; i++) { out[i] = Number(n & 0xFFn); n >>= 8n; }
  return out;
}

// --- Slice conversions (same pattern as schnorr.mjs) ---

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

// ScalarMult computes X25519(scalar, point).
// scalar and point are 32-byte little-endian. Returns 32-byte result.
export function ScalarMult(scalar, point) {
  const k = decodeScalar(sliceToU8(scalar));
  const u = decodeU(sliceToU8(point));
  return u8ToSlice(encodeU(ladder(k, u)));
}

// ScalarBaseMult computes X25519(scalar, 9).
export function ScalarBaseMult(scalar) {
  const k = decodeScalar(sliceToU8(scalar));
  return u8ToSlice(encodeU(ladder(k, 9n)));
}
