// TinyJS Runtime — SubtleCrypto bridge (AES-CBC, AES-GCM, PBKDF2, Argon2id, random bytes)

import { Slice } from './builtin.mjs';

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

function goStringToJS(s) {
  // Go string in tinyjs is either a JS string or {$val: string}
  if (typeof s === 'string') return s;
  if (s && typeof s.$val === 'string') return s.$val;
  return String(s);
}

export function RandomBytes(dst) {
  const u8 = new Uint8Array(dst.$length);
  crypto.getRandomValues(u8);
  for (let i = 0; i < dst.$length; i++) dst.$array[dst.$offset + i] = u8[i];
}

export function AESCBCEncrypt(key, iv, plaintext, fn) {
  const k = sliceToU8(key), v = sliceToU8(iv), pt = sliceToU8(plaintext);
  crypto.subtle.importKey('raw', k, { name: 'AES-CBC' }, false, ['encrypt'])
    .then(ck => crypto.subtle.encrypt({ name: 'AES-CBC', iv: v }, ck, pt))
    .then(buf => fn(u8ToSlice(new Uint8Array(buf))));
}

export function AESCBCDecrypt(key, iv, ciphertext, fn) {
  const k = sliceToU8(key), v = sliceToU8(iv), ct = sliceToU8(ciphertext);
  crypto.subtle.importKey('raw', k, { name: 'AES-CBC' }, false, ['decrypt'])
    .then(ck => crypto.subtle.decrypt({ name: 'AES-CBC', iv: v }, ck, ct))
    .then(buf => fn(u8ToSlice(new Uint8Array(buf))))
    .catch(() => fn(u8ToSlice(new Uint8Array(0))));
}

export function AESGCMEncrypt(key, iv, plaintext, fn) {
  const k = sliceToU8(key), v = sliceToU8(iv), pt = sliceToU8(plaintext);
  crypto.subtle.importKey('raw', k, { name: 'AES-GCM' }, false, ['encrypt'])
    .then(ck => crypto.subtle.encrypt({ name: 'AES-GCM', iv: v }, ck, pt))
    .then(buf => fn(u8ToSlice(new Uint8Array(buf))))
    .catch(() => fn(u8ToSlice(new Uint8Array(0))));
}

export function AESGCMDecrypt(key, iv, ciphertext, fn) {
  const k = sliceToU8(key), v = sliceToU8(iv), ct = sliceToU8(ciphertext);
  crypto.subtle.importKey('raw', k, { name: 'AES-GCM' }, false, ['decrypt'])
    .then(ck => crypto.subtle.decrypt({ name: 'AES-GCM', iv: v }, ck, ct))
    .then(buf => fn(u8ToSlice(new Uint8Array(buf))))
    .catch(() => fn(u8ToSlice(new Uint8Array(0))));
}

export function PBKDF2DeriveKey(password, salt, iterations, fn) {
  const pw = goStringToJS(password);
  const s = sliceToU8(salt);
  const enc = new TextEncoder();
  crypto.subtle.importKey('raw', enc.encode(pw), 'PBKDF2', false, ['deriveBits'])
    .then(baseKey => crypto.subtle.deriveBits(
      { name: 'PBKDF2', salt: s, iterations: iterations, hash: 'SHA-256' },
      baseKey, 256))
    .then(bits => fn(u8ToSlice(new Uint8Array(bits))))
    .catch(() => fn(u8ToSlice(new Uint8Array(0))));
}

export function SHA256Hex(data, fn) {
  const d = sliceToU8(data);
  crypto.subtle.digest('SHA-256', d).then(buf => {
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
