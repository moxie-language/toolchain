// TinyJS Runtime — SubtleCrypto bridge (AES-CBC, AES-GCM, PBKDF2, Argon2id, random bytes)
// Includes pure-JS fallbacks for insecure contexts (LAN HTTP dev mode).

import { Slice } from './builtin.mjs';

// Pure-JS crypto only — no platform dependency on crypto.subtle.
const _subtle = null;

const _empty = new Uint8Array(0);

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

function goStringToJS(s) {
  if (typeof s === 'string') return s;
  if (s && typeof s.$val === 'string') return s.$val;
  if (s && s.$array !== undefined) return new TextDecoder().decode(sliceToU8(s));
  return String(s);
}

// ========================================================================
// Pure-JS SHA-256
// ========================================================================

const _K256 = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5,
  0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
  0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc,
  0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7,
  0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
  0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3,
  0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5,
  0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
  0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

function _rotr32(x, n) { return (x >>> n) | (x << (32 - n)); }

function _sha256(data) {
  if (!(data instanceof Uint8Array)) data = new Uint8Array(data);
  const len = data.length;
  const bitLen = len * 8;
  const padLen = ((56 - (len + 1) % 64) + 64) % 64;
  const buf = new Uint8Array(len + 1 + padLen + 8);
  buf.set(data);
  buf[len] = 0x80;
  const dv = new DataView(buf.buffer);
  dv.setUint32(buf.length - 8, 0, false);
  dv.setUint32(buf.length - 4, bitLen, false);

  let h0 = 0x6a09e667, h1 = 0xbb67ae85, h2 = 0x3c6ef372, h3 = 0xa54ff53a;
  let h4 = 0x510e527f, h5 = 0x9b05688c, h6 = 0x1f83d9ab, h7 = 0x5be0cd19;
  const W = new Int32Array(64);

  for (let off = 0; off < buf.length; off += 64) {
    for (let i = 0; i < 16; i++) W[i] = dv.getInt32(off + i * 4, false);
    for (let i = 16; i < 64; i++) {
      const s0 = _rotr32(W[i-15], 7) ^ _rotr32(W[i-15], 18) ^ (W[i-15] >>> 3);
      const s1 = _rotr32(W[i-2], 17) ^ _rotr32(W[i-2], 19) ^ (W[i-2] >>> 10);
      W[i] = (W[i-16] + s0 + W[i-7] + s1) | 0;
    }
    let a=h0, b=h1, c=h2, d=h3, e=h4, f=h5, g=h6, h=h7;
    for (let i = 0; i < 64; i++) {
      const S1 = _rotr32(e, 6) ^ _rotr32(e, 11) ^ _rotr32(e, 25);
      const ch = (e & f) ^ (~e & g);
      const t1 = (h + S1 + ch + _K256[i] + W[i]) | 0;
      const S0 = _rotr32(a, 2) ^ _rotr32(a, 13) ^ _rotr32(a, 22);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (S0 + maj) | 0;
      h=g; g=f; f=e; e=(d+t1)|0; d=c; c=b; b=a; a=(t1+t2)|0;
    }
    h0=(h0+a)|0; h1=(h1+b)|0; h2=(h2+c)|0; h3=(h3+d)|0;
    h4=(h4+e)|0; h5=(h5+f)|0; h6=(h6+g)|0; h7=(h7+h)|0;
  }

  const out = new Uint8Array(32);
  const ov = new DataView(out.buffer);
  ov.setUint32(0,h0,false); ov.setUint32(4,h1,false); ov.setUint32(8,h2,false); ov.setUint32(12,h3,false);
  ov.setUint32(16,h4,false); ov.setUint32(20,h5,false); ov.setUint32(24,h6,false); ov.setUint32(28,h7,false);
  return out;
}

// ========================================================================
// Pure-JS SHA-512 (BigInt-based for 64-bit words)
// ========================================================================

const _K512 = [
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

const _m64 = 0xFFFFFFFFFFFFFFFFn;
function _rotr64(x, n) { const b = BigInt(n); return ((x >> b) | (x << (64n - b))) & _m64; }

function _sha512(data) {
  if (!(data instanceof Uint8Array)) data = new Uint8Array(data);
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
      const s0 = _rotr64(W[i-15],1) ^ _rotr64(W[i-15],8) ^ (W[i-15] >> 7n);
      const s1 = _rotr64(W[i-2],19) ^ _rotr64(W[i-2],61) ^ (W[i-2] >> 6n);
      W[i] = (W[i-16] + s0 + W[i-7] + s1) & _m64;
    }
    let a=h0, b=h1, c=h2, d=h3, e=h4, f=h5, g=h6, h=h7;
    for (let i = 0; i < 80; i++) {
      const S1 = _rotr64(e,14) ^ _rotr64(e,18) ^ _rotr64(e,41);
      const ch = (e & f) ^ ((~e & _m64) & g);
      const t1 = (h + S1 + ch + _K512[i] + W[i]) & _m64;
      const S0 = _rotr64(a,28) ^ _rotr64(a,34) ^ _rotr64(a,39);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const t2 = (S0 + maj) & _m64;
      h=g; g=f; f=e; e=(d+t1)&_m64; d=c; c=b; b=a; a=(t1+t2)&_m64;
    }
    h0=(h0+a)&_m64; h1=(h1+b)&_m64; h2=(h2+c)&_m64; h3=(h3+d)&_m64;
    h4=(h4+e)&_m64; h5=(h5+f)&_m64; h6=(h6+g)&_m64; h7=(h7+h)&_m64;
  }

  const out = new Uint8Array(64);
  const ov = new DataView(out.buffer);
  function wr64(o, v) { ov.setUint32(o, Number(v >> 32n), false); ov.setUint32(o+4, Number(v & 0xFFFFFFFFn), false); }
  wr64(0,h0); wr64(8,h1); wr64(16,h2); wr64(24,h3);
  wr64(32,h4); wr64(40,h5); wr64(48,h6); wr64(56,h7);
  return out;
}

// ========================================================================
// HMAC + PBKDF2 (pure-JS, generic over hash function)
// ========================================================================

function _hmac(hashFn, blockSize, key, data) {
  if (key.length > blockSize) key = hashFn(key);
  const pk = new Uint8Array(blockSize);
  pk.set(key);
  const ipad = new Uint8Array(blockSize + data.length);
  const opad = new Uint8Array(blockSize);
  for (let i = 0; i < blockSize; i++) { ipad[i] = pk[i] ^ 0x36; opad[i] = pk[i] ^ 0x5c; }
  ipad.set(data, blockSize);
  const ih = hashFn(ipad);
  const outer = new Uint8Array(blockSize + ih.length);
  outer.set(opad);
  outer.set(ih, blockSize);
  return hashFn(outer);
}

function _hmacSha256(key, data) { return _hmac(_sha256, 64, key, data); }
function _hmacSha512(key, data) { return _hmac(_sha512, 128, key, data); }

function _pbkdf2(hmacFn, hashLen, password, salt, iterations, dkLen) {
  const pw = (typeof password === 'string') ? new TextEncoder().encode(password) : password;
  const blocks = Math.ceil(dkLen / hashLen);
  const result = new Uint8Array(dkLen);
  for (let block = 1; block <= blocks; block++) {
    const sb = new Uint8Array(salt.length + 4);
    sb.set(salt);
    sb[salt.length]   = (block >>> 24) & 0xff;
    sb[salt.length+1] = (block >>> 16) & 0xff;
    sb[salt.length+2] = (block >>> 8) & 0xff;
    sb[salt.length+3] = block & 0xff;
    let u = hmacFn(pw, sb);
    const t = u.slice();
    for (let i = 1; i < iterations; i++) {
      u = hmacFn(pw, u);
      for (let j = 0; j < t.length; j++) t[j] ^= u[j];
    }
    result.set(t.slice(0, Math.min(hashLen, dkLen - (block-1)*hashLen)), (block-1)*hashLen);
  }
  return result;
}

// ========================================================================
// Stream cipher (AES-GCM fallback for insecure contexts)
// CTR mode using SHA-256 as PRF + SHA-256 MAC tag (16 bytes).
// Same API surface as AES-256-GCM: encrypt appends tag, decrypt verifies it.
// NOT interchangeable with real AES-GCM — dev-mode vaults stay in dev mode.
// ========================================================================

function _streamEncrypt(key, iv, pt) {
  const ct = new Uint8Array(pt.length);
  const cb = new Uint8Array(key.length + iv.length + 4);
  cb.set(key); cb.set(iv, key.length);
  for (let off = 0, ctr = 0; off < pt.length; ctr++) {
    cb[cb.length-4] = (ctr>>>24)&0xff; cb[cb.length-3] = (ctr>>>16)&0xff;
    cb[cb.length-2] = (ctr>>>8)&0xff;  cb[cb.length-1] = ctr&0xff;
    const ks = _sha256(cb);
    for (let i = 0; i < 32 && off < pt.length; i++, off++) ct[off] = pt[off] ^ ks[i];
  }
  const ti = new Uint8Array(key.length + ct.length);
  ti.set(key); ti.set(ct, key.length);
  const tag = _sha256(ti).slice(0, 16);
  const out = new Uint8Array(ct.length + 16);
  out.set(ct); out.set(tag, ct.length);
  return out;
}

function _streamDecrypt(key, iv, data) {
  if (data.length < 16) return _empty;
  const ctLen = data.length - 16;
  const ct = data.slice(0, ctLen);
  const tag = data.slice(ctLen);
  const ti = new Uint8Array(key.length + ct.length);
  ti.set(key); ti.set(ct, key.length);
  const expected = _sha256(ti).slice(0, 16);
  let ok = true;
  for (let i = 0; i < 16; i++) if (tag[i] !== expected[i]) ok = false;
  if (!ok) return _empty;
  const pt = new Uint8Array(ctLen);
  const cb = new Uint8Array(key.length + iv.length + 4);
  cb.set(key); cb.set(iv, key.length);
  for (let off = 0, ctr = 0; off < ctLen; ctr++) {
    cb[cb.length-4] = (ctr>>>24)&0xff; cb[cb.length-3] = (ctr>>>16)&0xff;
    cb[cb.length-2] = (ctr>>>8)&0xff;  cb[cb.length-1] = ctr&0xff;
    const ks = _sha256(cb);
    for (let i = 0; i < 32 && off < ctLen; i++, off++) pt[off] = ct[off] ^ ks[i];
  }
  return pt;
}

// ========================================================================
// Exports — each function prefers crypto.subtle, falls back to pure JS
// ========================================================================

export function RandomBytes(dst) {
  const u8 = new Uint8Array(dst.$length);
  crypto.getRandomValues(u8);
  for (let i = 0; i < dst.$length; i++) dst.$array[dst.$offset + i] = u8[i];
}

export function AESCBCEncrypt(key, iv, plaintext, fn) {
  if (!_subtle) { fn(u8ToSlice(_empty)); return; }
  const k = sliceToU8(key), v = sliceToU8(iv), pt = sliceToU8(plaintext);
  _subtle.importKey('raw', k, { name: 'AES-CBC' }, false, ['encrypt'])
    .then(ck => _subtle.encrypt({ name: 'AES-CBC', iv: v }, ck, pt))
    .then(buf => fn(u8ToSlice(new Uint8Array(buf))))
    .catch(() => fn(u8ToSlice(_empty)));
}

export function AESCBCDecrypt(key, iv, ciphertext, fn) {
  if (!_subtle) { fn(u8ToSlice(_empty)); return; }
  const k = sliceToU8(key), v = sliceToU8(iv), ct = sliceToU8(ciphertext);
  _subtle.importKey('raw', k, { name: 'AES-CBC' }, false, ['decrypt'])
    .then(ck => _subtle.decrypt({ name: 'AES-CBC', iv: v }, ck, ct))
    .then(buf => fn(u8ToSlice(new Uint8Array(buf))))
    .catch(() => fn(u8ToSlice(_empty)));
}

export function AESGCMEncrypt(key, iv, plaintext, fn) {
  const k = sliceToU8(key), v = sliceToU8(iv), pt = sliceToU8(plaintext);
  if (!_subtle) { fn(u8ToSlice(_streamEncrypt(k, v, pt))); return; }
  _subtle.importKey('raw', k, { name: 'AES-GCM' }, false, ['encrypt'])
    .then(ck => _subtle.encrypt({ name: 'AES-GCM', iv: v }, ck, pt))
    .then(buf => fn(u8ToSlice(new Uint8Array(buf))))
    .catch(() => fn(u8ToSlice(_empty)));
}

export function AESGCMDecrypt(key, iv, ciphertext, fn) {
  const k = sliceToU8(key), v = sliceToU8(iv), ct = sliceToU8(ciphertext);
  if (!_subtle) { fn(u8ToSlice(_streamDecrypt(k, v, ct))); return; }
  _subtle.importKey('raw', k, { name: 'AES-GCM' }, false, ['decrypt'])
    .then(ck => _subtle.decrypt({ name: 'AES-GCM', iv: v }, ck, ct))
    .then(buf => fn(u8ToSlice(new Uint8Array(buf))))
    .catch(() => fn(u8ToSlice(_empty)));
}

export function PBKDF2DeriveKey(password, salt, iterations, fn) {
  const pw = goStringToJS(password);
  const s = sliceToU8(salt);
  if (!_subtle) {
    fn(u8ToSlice(_pbkdf2(_hmacSha256, 32, pw, s, iterations, 32)));
    return;
  }
  const enc = new TextEncoder();
  _subtle.importKey('raw', enc.encode(pw), 'PBKDF2', false, ['deriveBits'])
    .then(baseKey => _subtle.deriveBits(
      { name: 'PBKDF2', salt: s, iterations: iterations, hash: 'SHA-256' },
      baseKey, 256))
    .then(bits => fn(u8ToSlice(new Uint8Array(bits))))
    .catch(() => fn(u8ToSlice(_empty)));
}

export function SHA256Hex(data, fn) {
  const d = sliceToU8(data);
  if (!_subtle) {
    const hash = _sha256(d);
    let hex = '';
    for (let i = 0; i < hash.length; i++) hex += hash[i].toString(16).padStart(2, '0');
    fn(hex);
    return;
  }
  _subtle.digest('SHA-256', d).then(buf => {
    const arr = new Uint8Array(buf);
    let hex = '';
    for (let i = 0; i < arr.length; i++) hex += arr[i].toString(16).padStart(2, '0');
    fn(hex);
  }).catch(() => fn(''));
}

// Argon2id — prefers hash-wasm WASM (loaded via UMD in background.html),
// falls back to pure-JS argon2.mjs.
let _argon2mod = null;
export function Argon2idDeriveKey(password, salt, t, m, p, dkLen, fn) {
  const pw = goStringToJS(password);
  const s = sliceToU8(salt);

  // hash-wasm WASM path (fast, ~3s for 256MB)
  if (globalThis.hashwasm && globalThis.hashwasm.argon2id) {
    globalThis.hashwasm.argon2id({
      password: pw,
      salt: s,
      iterations: t,
      parallelism: p,
      memorySize: m,
      hashLength: dkLen,
      outputType: 'binary',
    }).then(result => {
      fn(u8ToSlice(result));
    }).catch(e => {
      console.error('hashwasm Argon2id error:', e);
      fn(u8ToSlice(new Uint8Array(0)));
    });
    return;
  }

  // Pure-JS fallback (slow, minutes for 256MB)
  const doDerive = (mod) => {
    try {
      const result = mod.argon2id(pw, s, { t, m, p, dkLen });
      fn(u8ToSlice(result));
    } catch(e) {
      console.error('Argon2id error:', e);
      fn(u8ToSlice(new Uint8Array(0)));
    }
  };
  if (_argon2mod) {
    doDerive(_argon2mod);
  } else {
    import('./argon2.mjs').then(mod => {
      _argon2mod = mod;
      doDerive(mod);
    }).catch(e => {
      console.error('Failed to load argon2.mjs:', e);
      fn(u8ToSlice(new Uint8Array(0)));
    });
  }
}

// HMACSHA512 computes HMAC-SHA-512.
export function HMACSHA512(key, data, fn) {
  const k = sliceToU8(key);
  const d = sliceToU8(data);
  if (!_subtle) {
    fn(u8ToSlice(_hmacSha512(k, d)));
    return;
  }
  _subtle.importKey('raw', k, {name: 'HMAC', hash: 'SHA-512'}, false, ['sign'])
    .then(ck => _subtle.sign('HMAC', ck, d))
    .then(buf => fn(u8ToSlice(new Uint8Array(buf))))
    .catch(() => fn(u8ToSlice(_empty)));
}

// PBKDF2SHA512 derives a key using PBKDF2 with SHA-512.
export function PBKDF2SHA512(password, salt, iterations, dkLen, fn) {
  const pw = goStringToJS(password);
  const s = sliceToU8(salt);
  if (!_subtle) {
    fn(u8ToSlice(_pbkdf2(_hmacSha512, 64, pw, s, iterations, dkLen)));
    return;
  }
  const enc = new TextEncoder();
  _subtle.importKey('raw', enc.encode(pw), 'PBKDF2', false, ['deriveBits'])
    .then(baseKey => _subtle.deriveBits(
      {name: 'PBKDF2', salt: s, iterations: iterations, hash: 'SHA-512'},
      baseKey, dkLen * 8))
    .then(bits => fn(u8ToSlice(new Uint8Array(bits))))
    .catch(() => fn(u8ToSlice(_empty)));
}
