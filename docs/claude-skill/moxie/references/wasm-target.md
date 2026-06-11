# Moxie WASM Target

## String Pragma

In WASM, the compiler preserves source spelling of `string` vs `[]byte`:
- `string` -> native JS string at WASM/JS boundary
- `[]byte` -> byte-array object

A `[]byte` field receiving text at the boundary will **corrupt multi-byte UTF-8** (CJK, emoji) silently.

**Rule: declare all text-holding struct fields as `string` when targeting WASM.**

This is not a type distinction - they remain the same type and are mutually assignable. The spelling is a code generator hint only.

## JS Bridge Packages

| Bridge | Purpose |
|--------|---------|
| `dom` | Element creation, tree manipulation, events |
| `ws` | WebSocket dial/send/close |
| `sw` | Service Worker lifecycle, fetch/cache, SSE |
| `localstorage` | localStorage operations |
| `idb` | IndexedDB transactions |
| `crypto` | secp256k1 (schnorr / BIP340) via WASM |
| `subtle` | SubtleCrypto bridge |

## JS Runtime Shims (jsruntime/)

Helper modules used as WASM host shims. Not importable from Moxie source; wired into WASM bridge code:

| Shim | Purpose |
|------|---------|
| `aead.mjs` | XChaCha20-Poly1305 |
| `p256.mjs` | NIST P-256 ECDH / ECDSA |
| `ed25519.mjs` | Ed25519 signatures |
| `x25519.mjs` | X25519 ECDH |
| `poly1305.mjs` | Poly1305 MAC |

## Stdlib Crypto (native + WASM)

| Package | Purpose |
|---------|---------|
| `crypto/ec/secp256k1` | Bitcoin curve operations |
| `crypto/ec/schnorr` | BIP340 Schnorr signatures |
| `crypto/ec/bech32` | Bech32 / Bech32m encoding |
| `crypto/ec/ecdsa` | secp256k1 ECDSA |
| `crypto/ec/chainhash` | Double-SHA-256 |
| `encoding/base58` | base58 / base58check |

## WASM Memory

The WASM target uses Boehm conservative GC with recursive depth-first mark phase. Fixed WASM call stack (~1MB = ~10k-20k frames). Large live heap graphs overflow the stack.

Do not write "allocate freely, GC will clean up" code in long-lived WASM workers. Every allocation needs an explicit free path.

## spawn in WASM

Domains are Web Workers. IPC via `BroadcastChannel`. Slices element-copied, maps shallow-copied. Each domain gets its own event loop and isolated microtask context.
