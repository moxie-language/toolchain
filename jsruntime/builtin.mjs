// TinyJS Runtime — Builtin Operations
// Slice, map, string operations that mirror Go's builtin functions.

// --- Slices ---

export class Slice {
  constructor(array, offset, length, capacity, secure) {
    this.$array = array;     // backing array (JS Array)
    this.$offset = offset;   // start offset into backing array
    this.$length = length;   // number of accessible elements
    this.$capacity = capacity; // max elements before realloc
    // $secure is the taint bit for the `secure` slice allocation marker.
    // When truthy, comparisons involving this slice use constant-time
    // byte equality/ordering and any new slice derived from this one
    // (sub-slice, append, copy destination, concat) inherits the bit.
    this.$secure = !!secure;
  }

  get(i) {
    if (i < 0 || i >= this.$length) {
      throw new Error(`runtime error: index out of range [${i}] with length ${this.$length}`);
    }
    return this.$array[this.$offset + i];
  }

  set(i, v) {
    if (i < 0 || i >= this.$length) {
      throw new Error(`runtime error: index out of range [${i}] with length ${this.$length}`);
    }
    this.$array[this.$offset + i] = v;
  }

  addr(i) {
    if (i < 0 || i >= this.$length) {
      throw new Error(`runtime error: index out of range [${i}] with length ${this.$length}`);
    }
    const arr = this.$array;
    const idx = this.$offset + i;
    return {
      $get: () => arr[idx],
      $set: (v) => { arr[idx] = v; }
    };
  }

  // Decode bytes to a JS string so that `+` concatenation works.
  // In Moxie string and []byte are the same type — Slice objects must
  // coerce to JS strings when used with the + operator.
  toString() {
    const buf = new Uint8Array(this.$length);
    for (let i = 0; i < this.$length; i++) {
      buf[i] = this.$array[this.$offset + i];
    }
    return new TextDecoder().decode(buf);
  }
}

// Make a new slice. Object zero values must be deep-cloned per slot — otherwise
// every slot shares one reference and mutating any element mutates all of them.
export function makeSlice(len, cap, zero) {
  if (cap === undefined || cap < len) cap = len;
  const arr = new Array(cap);
  const needsClone = zero !== undefined && zero !== null && typeof zero === 'object';
  for (let i = 0; i < cap; i++) {
    arr[i] = needsClone ? cloneValue(zero) : (zero !== undefined ? zero : 0);
  }
  return new Slice(arr, 0, len, cap);
}

// secureAlloc is the JS-target implementation of the Moxie `[]byte{:n, secure}`
// literal. On native targets this maps onto mmap+mlock guarded arenas with
// signal-handler wipe; the browser has no such primitives so the allocator
// degrades to a zeroed []byte slice carrying the $secure taint bit. The
// taint flips subsequent __moxie_eq / __moxie_lt onto the constant-time
// path and propagates through sliceSlice/append/copy/stringConcat so that
// any derived buffer retains the comparison guarantee.
export function secureAlloc(n) {
  const arr = new Array(n);
  for (let i = 0; i < n; i++) arr[i] = 0;
  return new Slice(arr, 0, n, n, true);
}

// Slice a slice: s[low:high:max]
export function sliceSlice(s, low, high, max) {
  if (low === undefined) low = 0;
  if (high === undefined) high = s.$length;
  if (max === undefined) max = s.$capacity;

  if (low < 0 || high < low || max < high || max > s.$capacity) {
    throw new Error(`runtime error: slice bounds out of range [${low}:${high}:${max}] with capacity ${s.$capacity}`);
  }

  // Slicing a secure slice produces a secure sub-slice — the taint bit
  // follows the backing bytes, not the slice header.
  return new Slice(s.$array, s.$offset + low, high - low, max - low, s.$secure);
}

// Append to slice.
export function append(s, ...elems) {
  if (s == null) {
    s = new Slice([], 0, 0, 0);
  }

  const needed = s.$length + elems.length;
  const secure = !!s.$secure;

  if (needed <= s.$capacity) {
    // Fits in existing backing array.
    for (let i = 0; i < elems.length; i++) {
      s.$array[s.$offset + s.$length + i] = elems[i];
    }
    return new Slice(s.$array, s.$offset, needed, s.$capacity, secure);
  }

  // Need to grow. Go growth strategy: double until 256, then grow by 25%.
  let newCap = s.$capacity;
  if (newCap === 0) newCap = 1;
  while (newCap < needed) {
    if (newCap < 256) {
      newCap *= 2;
    } else {
      newCap += Math.floor(newCap / 4);
    }
  }

  const newArr = new Array(newCap);
  for (let i = 0; i < s.$length; i++) {
    newArr[i] = s.$array[s.$offset + i];
  }
  for (let i = 0; i < elems.length; i++) {
    newArr[s.$length + i] = elems[i];
  }

  return new Slice(newArr, 0, needed, newCap, secure);
}

// Append a string's UTF-8 bytes to a byte slice: append([]byte, string...)
export function appendString(dst, s) {
  const bytes = utf8Bytes(s);
  const elems = [];
  for (let i = 0; i < bytes.length; i++) elems.push(bytes[i]);
  return append(dst, ...elems);
}

// Append slice to slice: append(a, b...)
export function appendSlice(dst, src) {
  if (src === null || src === undefined || src.$length === 0) return dst;
  const elems = [];
  for (let i = 0; i < src.$length; i++) {
    elems.push(src.$array[src.$offset + i]);
  }
  const result = append(dst, ...elems);
  // Taint propagation: if src was secure, the appended bytes carry secret
  // material into dst. Mark the result secure so downstream comparisons
  // still run constant-time.
  if (result && (src.$secure || (dst && dst.$secure))) {
    result.$secure = true;
  }
  return result;
}

// Copy from src to dst. Returns number of elements copied.
// nil src / dst match Go semantics: zero-length, no-op copy.
export function copy(dst, src) {
  if (dst === null || dst === undefined) return 0;
  if (src === null || src === undefined) return 0;
  // Handle string source — copy UTF-8 bytes.
  if (typeof src === 'string') {
    const bytes = utf8Bytes(src);
    const n = Math.min(dst.$length, bytes.length);
    for (let i = 0; i < n; i++) {
      dst.$array[dst.$offset + i] = bytes[i];
    }
    return n;
  }

  const n = Math.min(dst.$length, src.$length);
  // Handle overlapping slices.
  if (dst.$array === src.$array && dst.$offset > src.$offset) {
    for (let i = n - 1; i >= 0; i--) {
      dst.$array[dst.$offset + i] = src.$array[src.$offset + i];
    }
  } else {
    for (let i = 0; i < n; i++) {
      dst.$array[dst.$offset + i] = src.$array[src.$offset + i];
    }
  }
  // Taint propagation: copying secret bytes into dst makes dst secret.
  // The $secure flag flips on dst's header, so any subsequent comparison
  // where dst is an operand takes the constant-time path — closing the
  // `copy(temp, secret); if temp == received { ... }` side-channel.
  if (src.$secure) {
    dst.$secure = true;
  }
  return n;
}

// Len.
export function len(v) {
  if (v === null || v === undefined) return 0;
  if (typeof v === 'string') return utf8Bytes(v).length;
  if (v instanceof Slice) return v.$length;
  if (v instanceof Map) return v.size;
  if (v instanceof GoMap) return v.size();
  if (Array.isArray(v)) return v.length;
  return 0;
}

// Cap.
export function cap(v) {
  if (v === null || v === undefined) return 0;
  if (v instanceof Slice) return v.$capacity;
  if (Array.isArray(v)) return v.length;
  return 0;
}

// --- Maps ---

// Go maps need special key handling (struct keys use deep equality).
export class GoMap {
  constructor() {
    this.entries = []; // [{key, value, hash}]
  }

  get(key) {
    for (const e of this.entries) {
      if (deepEqual(e.key, key)) return { value: e.value, ok: true };
    }
    return { value: undefined, ok: false };
  }

  set(key, value) {
    for (const e of this.entries) {
      if (deepEqual(e.key, key)) {
        e.value = value;
        return;
      }
    }
    this.entries.push({ key, value });
  }

  delete(key) {
    const idx = this.entries.findIndex(e => deepEqual(e.key, key));
    if (idx >= 0) this.entries.splice(idx, 1);
  }

  has(key) {
    return this.entries.some(e => deepEqual(e.key, key));
  }

  size() {
    return this.entries.length;
  }

  // Iterate: calls fn(key, value) for each entry.
  // Order is randomized per Go spec.
  forEach(fn) {
    // Randomize iteration order.
    const shuffled = [...this.entries];
    for (let i = shuffled.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1));
      [shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]];
    }
    for (const e of shuffled) {
      fn(e.key, e.value);
    }
  }
}

// For simple key types (string, number, bool), use native Map.
export function makeMap(keyKind) {
  if (keyKind === 'string' || keyKind === 'int' || keyKind === 'float64' || keyKind === 'bool') {
    return new Map();
  }
  return new GoMap();
}

// Map lookup with comma-ok.
export function mapLookup(m, key) {
  if (m === null || m === undefined) return { value: null, ok: false };
  if (m instanceof Map) {
    if (m.has(key)) return { value: m.get(key), ok: true };
    return { value: null, ok: false };
  }
  if (m instanceof GoMap) return m.get(key);
  return { value: null, ok: false };
}

// Map update.
export function mapUpdate(m, key, value) {
  if (m === null || m === undefined) {
    throw new Error('assignment to entry in nil map');
  }
  if (m instanceof Map) {
    m.set(key, value);
  } else if (m instanceof GoMap) {
    m.set(key, value);
  }
}

// Map delete.
export function mapDelete(m, key) {
  if (m === null || m === undefined) return;
  if (m instanceof Map) {
    m.delete(key);
  } else if (m instanceof GoMap) {
    m.delete(key);
  }
}

// Builtin clear(slice) — zeros every element in [0, length).
// This is the JS-side wipe primitive for the secure-allocation contract:
// `[]byte{:n, secure}` gives you a best-effort allocation on JS, and
// `clear(b)` is the caller's way of overwriting the backing before it
// falls out of scope. On []byte the fill value is 0, matching Go semantics.
export function clearSlice(s) {
  if (s === null || s === undefined) return;
  const arr = s.$array;
  const off = s.$offset;
  const n = s.$length;
  for (let i = 0; i < n; i++) {
    arr[off + i] = 0;
  }
}

// Builtin clear(map) — deletes every entry.
export function clearMap(m) {
  if (m === null || m === undefined) return;
  if (m instanceof Map) {
    m.clear();
  } else if (m instanceof GoMap) {
    m.entries.length = 0;
  }
}

// --- Strings (UTF-8 byte semantics) ---

const _enc = new TextEncoder();
const _dec = new TextDecoder();

// One-string cache: amortizes TextEncoder cost when a loop indexes the same string.
let _cacheStr = '';
let _cacheBytes = new Uint8Array(0);

export function utf8Bytes(s) {
  if (typeof s !== 'string') s = '' + s; // unbox String objects for cache hits
  if (s !== _cacheStr) {
    _cacheStr = s;
    _cacheBytes = _enc.encode(s);
  }
  return _cacheBytes;
}

// UTF-8 byte length of a string.
export function byteLen(s) {
  return utf8Bytes(s).length;
}

// UTF-8 byte at byte position i.
export function stringByteAt(s, i) {
  return utf8Bytes(s)[i];
}

// for range over string — returns (byteIndex, rune) using UTF-8.
export function stringRange(s) {
  const bytes = utf8Bytes(s);
  return {
    $bytes: bytes,
    $pos: 0,
    next() {
      if (this.$pos >= this.$bytes.length) return [false, 0, 0];
      const start = this.$pos;
      const b0 = this.$bytes[this.$pos];
      let cp;
      if (b0 < 0x80) {
        cp = b0; this.$pos += 1;
      } else if (b0 < 0xE0) {
        cp = ((b0 & 0x1F) << 6) | (this.$bytes[this.$pos + 1] & 0x3F);
        this.$pos += 2;
      } else if (b0 < 0xF0) {
        cp = ((b0 & 0x0F) << 12) | ((this.$bytes[this.$pos + 1] & 0x3F) << 6) |
             (this.$bytes[this.$pos + 2] & 0x3F);
        this.$pos += 3;
      } else {
        cp = ((b0 & 0x07) << 18) | ((this.$bytes[this.$pos + 1] & 0x3F) << 12) |
             ((this.$bytes[this.$pos + 2] & 0x3F) << 6) | (this.$bytes[this.$pos + 3] & 0x3F);
        this.$pos += 4;
      }
      return [true, start, cp];
    }
  };
}

// String to byte slice.
export function stringToBytes(s) {
  const bytes = _enc.encode(s);
  const arr = Array.from(bytes);
  return new Slice(arr, 0, arr.length, arr.length);
}

// Byte slice to string.
export function bytesToString(sl) {
  if (typeof sl === 'string') return sl;
  const bytes = new Uint8Array(sl.$length);
  for (let i = 0; i < sl.$length; i++) {
    bytes[i] = sl.$array[sl.$offset + i];
  }
  return _dec.decode(bytes);
}

// String to rune slice.
export function stringToRunes(s) {
  const runes = [...s].map(c => c.codePointAt(0));
  return new Slice(runes, 0, runes.length, runes.length);
}

// Rune slice to string.
export function runesToString(sl) {
  let s = '';
  for (let i = 0; i < sl.$length; i++) {
    s += String.fromCodePoint(sl.$array[sl.$offset + i]);
  }
  return s;
}

// sliceBytes extracts the underlying bytes from a Slice, string, or other
// text-like value without allocating a new string. Returns an Array of
// byte values or null if the input has no bytes. Used by the constant-time
// comparison paths so the comparison scans raw bytes rather than forcing
// UTF-8 decode/encode round-trips.
function sliceBytes(v) {
  if (v === null || v === undefined) return null;
  if (v instanceof Slice) {
    const out = new Array(v.$length);
    for (let i = 0; i < v.$length; i++) out[i] = v.$array[v.$offset + i];
    return out;
  }
  if (typeof v === 'string') {
    return Array.from(_enc.encode(v));
  }
  return null;
}

// isSecureOperand reports whether the value should force the
// constant-time comparison path. A Slice carrying $secure taints its
// own comparisons. A native JS string is never marked secure because
// strings have no stable header to carry the bit — callers must copy
// sensitive material into a `[]byte{:n, secure}` buffer before
// comparison for the guarantee to apply.
function isSecureOperand(v) {
  return v instanceof Slice && !!v.$secure;
}

// String concatenation (Go's + operator for strings).
// Propagates the $secure taint: if either operand is a secure Slice,
// the concatenated result is a secure Slice holding the combined bytes.
export function stringConcat(a, b) {
  if (isSecureOperand(a) || isSecureOperand(b)) {
    const ab = sliceBytes(a) || [];
    const bb = sliceBytes(b) || [];
    const arr = new Array(ab.length + bb.length);
    for (let i = 0; i < ab.length; i++) arr[i] = ab[i];
    for (let j = 0; j < bb.length; j++) arr[ab.length + j] = bb[j];
    return new Slice(arr, 0, arr.length, arr.length, true);
  }
  return a + b;
}

// constantTimeBytesEqual walks the full length of both inputs and
// accumulates differences into a running OR. Return value depends only
// on the final accumulator, so execution time is a function of (length +
// padding), never of where the first mismatch occurs. Mirrors the
// semantics of Go's subtle.ConstantTimeCompare / OpenSSL CRYPTO_memcmp.
function constantTimeBytesEqual(a, b) {
  const ab = sliceBytes(a);
  const bb = sliceBytes(b);
  if (ab === null || bb === null) return a === b;
  // Walk the max length so a shorter operand still contributes to the
  // accumulator via its virtual tail of zero-XOR-nonzero bytes.
  const n = ab.length >= bb.length ? ab.length : bb.length;
  let acc = (ab.length ^ bb.length) | 0;
  for (let i = 0; i < n; i++) {
    const x = i < ab.length ? ab[i] : 0;
    const y = i < bb.length ? bb[i] : 0;
    acc |= (x ^ y);
  }
  return acc === 0;
}

// String equality — handles both Slice objects and native JS strings.
// The JS backend emits this instead of === for string-typed operands,
// because Slice objects with identical content are !== each other.
//
// If either operand is a secure slice (carries $secure), the comparison
// runs in constant time over the maximum of the two lengths. Otherwise
// the fast path compares via string coercion.
export function stringEqual(a, b) {
  if (a === b) return true;
  if (isSecureOperand(a) || isSecureOperand(b)) {
    return constantTimeBytesEqual(a, b);
  }
  return ('' + a) === ('' + b);
}

// String comparison.
export function stringCompare(a, b) {
  if (isSecureOperand(a) || isSecureOperand(b)) {
    // Constant-time lexicographic compare. The loop body is data-
    // oblivious: all per-iteration branches fold into bitwise ops on
    // 32-bit integers, so per-byte cost is fixed regardless of content.
    //
    // The loop maintains:
    //   diff   — running OR of (x ^ y); once non-zero, a mismatch has
    //            been seen and subsequent iterations must not overwrite
    //            the result.
    //   result — -1 / 0 / 1, snapshotted at the first mismatch.
    //   seen   — 0 while diff == 0; -1 once diff != 0. Used as a mask
    //            to suppress updates to `result` after the first diff.
    const ab = sliceBytes(a) || [];
    const bb = sliceBytes(b) || [];
    const la = ab.length, lb = bb.length;
    const n = la >= lb ? la : lb;
    let diff = 0;
    let result = 0;
    for (let i = 0; i < n; i++) {
      const x = i < la ? ab[i] : 0;
      const y = i < lb ? bb[i] : 0;
      const d = (x - y) | 0;
      // seen: 0 while diff == 0, -1 once diff != 0
      const seen = -(((diff | -diff) >>> 31) | 0);
      // sign: -1 if d<0, 1 if d>0, 0 if d==0 — no branches
      const sign = (d >> 31) + (((-d) >>> 31) & 1);
      result = (result & seen) | (sign & ~seen);
      diff |= (x ^ y);
    }
    // Length tiebreaker when all compared bytes matched. Lengths are
    // non-secret in the Moxie model so this branch does not leak
    // byte content.
    if (diff === 0) {
      return la < lb ? -1 : (la > lb ? 1 : 0);
    }
    return result;
  }
  if (a < b) return -1;
  if (a > b) return 1;
  return 0;
}

// String slice: s[low:high] — operates on UTF-8 byte boundaries.
export function stringSlice(s, low, high) {
  const bytes = utf8Bytes(s);
  if (low === undefined) low = 0;
  if (high === undefined) high = bytes.length;
  return _dec.decode(bytes.subarray(low, high));
}

// --- String↔Slice compatibility shims ---
// moxiejs compiles s[i] to s.addr(i).$get() assuming Slice objects.
// DOM bridge and other JS APIs return raw strings — shim them to behave
// like read-only byte Slices so compiled code works on either type.
//
// addr() has its own byte cache, separate from utf8Bytes, so that
// stringSlice→utf8Bytes calls can't thrash the cache during byte loops.

let _addrStr = '';
let _addrBytes = new Uint8Array(0);

Object.defineProperty(String.prototype, 'addr', {
  value: function(i) {
    const s = '' + this;
    if (s !== _addrStr) { _addrStr = s; _addrBytes = _enc.encode(s); }
    const b = _addrBytes;
    return { $get: () => b[i], $set: () => {} };
  },
  writable: false, configurable: true, enumerable: false
});

Object.defineProperty(String.prototype, 'get', {
  value: function(i) {
    const s = '' + this;
    if (s !== _addrStr) { _addrStr = s; _addrBytes = _enc.encode(s); }
    return _addrBytes[i];
  },
  writable: false, configurable: true, enumerable: false
});

Object.defineProperty(String.prototype, '$length', {
  get: function() {
    const s = '' + this;
    if (s !== _addrStr) { _addrStr = s; _addrBytes = _enc.encode(s); }
    return _addrBytes.length;
  },
  configurable: true, enumerable: false
});

Object.defineProperty(String.prototype, '$capacity', {
  get: function() {
    const s = '' + this;
    if (s !== _addrStr) { _addrStr = s; _addrBytes = _enc.encode(s); }
    return _addrBytes.length;
  },
  configurable: true, enumerable: false
});

Object.defineProperty(String.prototype, '$offset', {
  get: function() { return 0; },
  configurable: true, enumerable: false
});

Object.defineProperty(String.prototype, '$array', {
  get: function() {
    const s = '' + this;
    if (s !== _addrStr) { _addrStr = s; _addrBytes = _enc.encode(s); }
    return _addrBytes;
  },
  configurable: true, enumerable: false
});

// --- Deep equality for map keys ---

function deepEqual(a, b) {
  if (a === b) return true;
  if (a === null || b === null) return a === b;
  if (typeof a !== typeof b) return false;
  if (typeof a !== 'object') return false;

  const keysA = Object.keys(a).filter(k => !k.startsWith('$'));
  const keysB = Object.keys(b).filter(k => !k.startsWith('$'));
  if (keysA.length !== keysB.length) return false;
  return keysA.every(k => deepEqual(a[k], b[k]));
}

// Clone a Go value type (array or struct). Slices get a new backing array copy.
// Structs get shallow field copies (nested value types need recursive clone).
// Pointer wrappers ({$get,$set}) are copied by reference — Go pointer-value
// semantics: struct-copy propagates the pointer bits, both structs then point
// to the same underlying allocation. Recursive-cloning a pointer wrapper as
// if it were a struct would strip all $-prefixed keys and return {}, silently
// erasing the pointer (caught in MLS framedContent struct copy).
export function cloneValue(v) {
  if (v === null || v === undefined || typeof v !== 'object') return v;
  if (v instanceof Slice) {
    const newArr = new Array(v.$capacity);
    for (let i = 0; i < v.$length; i++) {
      const elem = v.$array[v.$offset + i];
      newArr[i] = (typeof elem === 'object' && elem !== null) ? cloneValue(elem) : elem;
    }
    return new Slice(newArr, 0, v.$length, v.$capacity);
  }
  if (Array.isArray(v)) {
    return v.map(e => (typeof e === 'object' && e !== null) ? cloneValue(e) : e);
  }
  // Pointer wrapper: share by reference (Go pointer semantics).
  if (typeof v.$get === 'function' && typeof v.$set === 'function') {
    return v;
  }
  // Struct: shallow clone with recursive value cloning.
  const obj = {};
  for (const key of Object.keys(v)) {
    if (key.startsWith('$')) continue; // skip $get/$set/$value
    const val = v[key];
    obj[key] = (typeof val === 'object' && val !== null) ? cloneValue(val) : val;
  }
  return obj;
}

// Legacy 64-bit bitwise stubs — kept for compatibility with pre-BigInt compiled code.
// New code uses native BigInt operators directly.
export function int64or(x, y) { return x | y; }
export function int64and(x, y) { return x & y; }
export function int64xor(x, y) { return x ^ y; }
