// TinyJS Runtime — secp256k1 Crypto Bridge (WASM)
// init() must be called (and awaited) before any crypto operations.

import { Slice } from './builtin.mjs';

let _secp = null;

// init loads and initializes the secp256k1 WASM module.
// Called from $entry.mjs before main().
// Gracefully skips if WASM module is not available (e.g. app delegates crypto to extension).
export async function init() {
  try {
    const base = new URL('./', import.meta.url);
    const mod = await import(new URL('../$wasm/secp256k1.mjs', base).href);
    _secp = await mod.initSecp256k1();
  } catch (e) {
    console.log('crypto: secp256k1 WASM not available, crypto operations will use extension');
  }
}

function sliceToU8(s) {
  if (s instanceof Uint8Array) return s;
  const u = new Uint8Array(s.$length);
  for (let i = 0; i < s.$length; i++) {
    u[i] = s.$array[s.$offset + i];
  }
  return u;
}

function u8ToSlice(u8) {
  if (u8 === null || u8 === undefined) return null;
  const arr = new Array(u8.length);
  for (let i = 0; i < u8.length; i++) arr[i] = u8[i];
  return new Slice(arr, 0, u8.length, u8.length);
}

export function PubKeyFromSecKey(seckey) {
  const result = _secp.getPublicKey(sliceToU8(seckey));
  return u8ToSlice(result);
}

export function SignSchnorr(seckey, msg, auxRand) {
  const result = _secp.sign(sliceToU8(seckey), sliceToU8(msg), sliceToU8(auxRand));
  if (result === null || result === undefined) return null;
  return u8ToSlice(result);
}

export function VerifySchnorr(pubkey, msg, sig) {
  return _secp.verify(sliceToU8(pubkey), sliceToU8(msg), sliceToU8(sig));
}
