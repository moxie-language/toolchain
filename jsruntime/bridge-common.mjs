// bridge-common.mjs - shared wasm bridge infrastructure.
//
// Exports utility functions and a factory for the common bridge functions
// that appear identically in every wasm Worker host: SAB channel ops,
// spawn_domain, is_worker_context, subtle_random_bytes, core helpers, WASI.

// ── SAB ring buffer ────────────────────────────────────────────────────────

export const SAB_HDR = 32;
export const SAB_SLOT_HDR = 12;

export function sabCreate(slotSize, slotCount) {
  const sab = new SharedArrayBuffer(SAB_HDR + slotCount * slotSize);
  const u32 = new Uint32Array(sab);
  u32[2] = slotSize;
  u32[3] = slotCount;
  return sab;
}

// forceClose sets the closed flag and wakes all blocked waiters.
// No sentinel slot needed: send/recv wait loops check bit0 on every wakeup.
export function sabForceClose(sab) {
  const u32 = new Uint32Array(sab);
  Atomics.or(u32, 4, 1);
  Atomics.notify(u32, 0, Atomics.MAX_VALUE);
  Atomics.notify(u32, 1, Atomics.MAX_VALUE);
}

export function sabSend(sab, bytes) {
  const u32 = new Uint32Array(sab);
  const slotSize = u32[2], slotCount = u32[3];
  const payloadCap = slotSize - SAB_SLOT_HDR;
  if (bytes.length > (slotCount - 1) * payloadCap)
    throw new Error('channel_send: overflow');
  let offset = 0, first = true;
  while (offset <= bytes.length) {
    const chunkLen = Math.min(bytes.length - offset, payloadCap);
    const isFinal = (offset + chunkLen >= bytes.length) ? 1 : 0;
    let wi = Atomics.load(u32, 0);
    while (wi - Atomics.load(u32, 1) >= slotCount) {
      Atomics.wait(u32, 1, Atomics.load(u32, 1));
      wi = Atomics.load(u32, 0);
      if (Atomics.load(u32, 4) & 1) throw new Error('channel_send: send on closed channel');
    }
    if (Atomics.load(u32, 4) & 1) throw new Error('channel_send: send on closed channel');
    const base = SAB_HDR + (wi & (slotCount - 1)) * slotSize;
    const dv = new DataView(sab, base, slotSize);
    dv.setUint8(0, isFinal);
    dv.setUint8(1, 0); dv.setUint8(2, 0); dv.setUint8(3, 0);
    dv.setUint32(4, first ? bytes.length : 0, true);
    dv.setUint32(8, chunkLen, true);
    if (chunkLen > 0)
      new Uint8Array(sab, base + SAB_SLOT_HDR, chunkLen)
        .set(bytes.subarray(offset, offset + chunkLen));
    Atomics.store(u32, 0, wi + 1);
    Atomics.notify(u32, 0, 1);
    offset += chunkLen;
    first = false;
    if (isFinal) break;
  }
}

export function sabRecv(sab, dstBuf) {
  const u32 = new Uint32Array(sab);
  const slotSize = u32[2], slotCount = u32[3];
  let totalLen = -1, dstOffset = 0, awaitingFirst = true;
  for (;;) {
    let ri = Atomics.load(u32, 1);
    while (Atomics.load(u32, 0) === ri) {
      Atomics.wait(u32, 0, ri);
      ri = Atomics.load(u32, 1);
      if (Atomics.load(u32, 4) & 1) return -1;
    }
    const base = SAB_HDR + (ri & (slotCount - 1)) * slotSize;
    const dv = new DataView(sab, base, slotSize);
    const isFinal = dv.getUint8(0);
    const slotTotalLen = dv.getUint32(4, true);
    const chunkLen = dv.getUint32(8, true);
    Atomics.store(u32, 1, ri + 1);
    Atomics.notify(u32, 1, 1);
    if (awaitingFirst) {
      if (slotTotalLen === 0)
        return (Atomics.load(u32, 4) & 1) ? -1 : 0;
      if (slotTotalLen > dstBuf.length)
        throw new Error('channel_recv: overflow');
      totalLen = slotTotalLen;
      awaitingFirst = false;
    }
    if (chunkLen > 0) {
      dstBuf.set(new Uint8Array(sab, base + SAB_SLOT_HDR, chunkLen), dstOffset);
      dstOffset += chunkLen;
    }
    if (isFinal) return totalLen;
  }
}

export function sabClose(sab) {
  const u32 = new Uint32Array(sab);
  Atomics.or(u32, 4, 1);
  const slotSize = u32[2], slotCount = u32[3];
  let wi = Atomics.load(u32, 0);
  while (wi - Atomics.load(u32, 1) >= slotCount) {
    Atomics.wait(u32, 1, Atomics.load(u32, 1));
    wi = Atomics.load(u32, 0);
  }
  const base = SAB_HDR + (wi & (slotCount - 1)) * slotSize;
  const dv = new DataView(sab, base, slotSize);
  dv.setUint8(0, 1); dv.setUint32(4, 0, true); dv.setUint32(8, 0, true);
  Atomics.store(u32, 0, wi + 1);
  Atomics.notify(u32, 0, 1);
}

// ── fromSlice ──────────────────────────────────────────────────────────────

export function fromSlice(s) {
  if (s instanceof Uint8Array) return s;
  if (s && s.$array != null) {
    const u = new Uint8Array(s.$length);
    for (let i = 0; i < s.$length; i++) u[i] = s.$array[s.$offset + i];
    return u;
  }
  if (s && typeof s === 'string') return new TextEncoder().encode(s);
  return new Uint8Array(0);
}

// ── Core helpers factory ───────────────────────────────────────────────────

// makeCoreHelpers returns memory-access helpers that close over getMem/getXp.
// Call after building the bridge object; the getters provide the live wasm
// instance so helpers always see the current memory even after wasm growth.
export function makeCoreHelpers(getMem, getXp) {
  const enc = new TextEncoder();
  const dec = new TextDecoder();

  function readStr(ptr, len) {
    if (len <= 0) return '';
    return dec.decode(new Uint8Array(getMem().buffer, ptr >>> 0, len));
  }
  function readBytes(ptr, len) {
    if (len <= 0) return new Uint8Array(0);
    return new Uint8Array(getMem().buffer, ptr >>> 0, len);
  }
  function writeStr(s) {
    const bytes = enc.encode('' + s);
    const ptr = getXp().__alloc(bytes.length) >>> 0;
    new Uint8Array(getMem().buffer, ptr, bytes.length).set(bytes);
    return [ptr, bytes.length];
  }
  function writeI32(addr, val) {
    new DataView(getMem().buffer).setInt32(addr >>> 0, val, true);
  }
  function writeBytes(data) {
    const u = fromSlice(data);
    const ptr = getXp().__alloc(u.length) >>> 0;
    new Uint8Array(getMem().buffer, ptr, u.length).set(u);
    return [ptr, u.length];
  }
  function cb0(id) { getXp().__cb0(id); }
  function cbs(id, s) {
    const [ptr, len] = writeStr(s);
    getXp().__cbs(id, ptr, len);
  }
  function cbdata(id, data) {
    const [ptr, len] = writeBytes(data);
    getXp().__cbdata(id, ptr, len);
  }

  return { readStr, readBytes, writeStr, writeI32, writeBytes, cb0, cbs, cbdata };
}

// ── WASI ───────────────────────────────────────────────────────────────────

// makeEnv returns the 'env' import object for the host shims pkg/mxutil
// declares (write + the mxc_* file helpers). These are reached by app and
// module wasm binaries that import mxutil, so every instantiate call must
// provide the namespace or the module fails with
// "Import #1 env: module is not an object or function".
export function makeEnv(getMem) {
  const dec = new TextDecoder();
  const fail = () => -1;
  return {
    // No filesystem inside a worker: fail the file shims cleanly.
    mxc_readfile: fail,
    mxc_filesize: fail,
    mxc_listdir: fail,
    mxc_writefile: fail,
    mxc_mkdir: fail,
    mxc_getenv: fail,
    // write(fd, ptr, count): route 1/2 to the worker console.
    write(fd, ptr, count) {
      const n = count >>> 0;
      if ((fd === 1 || fd === 2) && n > 0) {
        const s = dec.decode(new Uint8Array(getMem().buffer, ptr >>> 0, n));
        if (fd === 1) console.log(s.replace(/\n$/, ''));
        else console.error(s.replace(/\n$/, ''));
      }
      return n;
    },
    // compiler-rt's 64x64 -> 128 multiply, and on wasm32 it is the ONLY place
    // a 128-bit product happens: LLVM folds the 32-bit-half algorithm in
    // math/bits.Mul64 into `mul i128`, which legalises to this call -
    // (retptr, aLo, aHi, bLo, bHi), the product written as two i64s.
    //
    // host.mjs (the page supervisor) has had this since the app's crypto was
    // found returning a correct LOW word and a garbage HIGH one; the WORKER
    // env object did not, so every worker module whose closure uses i128 - the
    // app bundle (ed25519 field arithmetic) and the signer - failed to LINK
    // with "LinkError: import object field '__multi3' is not a Function". The
    // app domain then sat in 'boot' forever: the failure is reported as an
    // error message, and the page's ready flag is never set. Phase 7's smoke
    // suite saw exactly that as five Selenium timeouts and three failures.
    __multi3(ret, aLo, aHi, bLo, bHi) {
      const M64 = 0xffffffffffffffffn;
      const a = (BigInt.asUintN(64, aHi) << 64n) | BigInt.asUintN(64, aLo);
      const b = (BigInt.asUintN(64, bHi) << 64n) | BigInt.asUintN(64, bLo);
      const p = a * b;
      const dv = new DataView(getMem().buffer);
      dv.setBigInt64(ret, BigInt.asIntN(64, p & M64), true);
      dv.setBigInt64(ret + 8, BigInt.asIntN(64, (p >> 64n) & M64), true);
      return 0;
    },
    'syscall.Exit'(code) {
      self.close();
      throw new Error('wasm exit ' + code);
    },
  };
}

export function makeWasi(getMem) {
  const dec = new TextDecoder();
  const _lineBuf = { 1: '', 2: '' };
  return {
    fd_write(fd, iovs, iovs_len, nwritten_ptr) {
      const dv = new DataView(getMem().buffer);
      let total = 0;
      const iovBase = iovs >>> 0;
      for (let i = 0; i < iovs_len; i++) {
        const ptr = dv.getUint32(iovBase + i * 8, true);
        const len = dv.getUint32(iovBase + i * 8 + 4, true);
        const s = dec.decode(new Uint8Array(getMem().buffer, ptr, len));
        if (fd === 1 || fd === 2) {
          _lineBuf[fd] += s;
          let nl;
          while ((nl = _lineBuf[fd].indexOf('\n')) !== -1) {
            const line = _lineBuf[fd].slice(0, nl);
            _lineBuf[fd] = _lineBuf[fd].slice(nl + 1);
            if (fd === 1) console.log(line);
            else console.error(line);
          }
        }
        total += len;
      }
      dv.setUint32(nwritten_ptr >>> 0, total, true);
      return 0;
    },
    // The app imports fd_read (os.Stdin read paths). There is no stdin in a
    // worker, so report EBADF: the runtime panics with a message naming the
    // missing descriptor, where a successful zero-byte read would just look
    // like an empty stream.
    fd_read(fd, iovs, iovs_len, nread_ptr) {
      const dv = new DataView(getMem().buffer);
      dv.setUint32(nread_ptr >>> 0, 0, true);
      return 8; // WASI errno EBADF
    },

    clock_time_get(clock_id, precision, time_ptr) {
      const dv = new DataView(getMem().buffer);
      const ms = (clock_id === 1) ? performance.now() : Date.now();
      const ns = BigInt(Math.trunc(ms * 1e6));
      dv.setBigUint64(time_ptr >>> 0, ns, true);
      return 0;
    },
  };
}

// ── Build hash ─────────────────────────────────────────────────────────────

export async function computeBuildHash(wasmBytes) {
  const buf = await crypto.subtle.digest('SHA-256', wasmBytes);
  return Array.from(new Uint8Array(buf))
    .map(b => b.toString(16).padStart(2, '0')).join('');
}

// ── Spawned Worker lifecycle ───────────────────────────────────────────────

export function createSpawnedWorker(wasmUrl, buildHash, fnIdx, argBytes, chanSabs, spawnedWorkerChans) {
  const worker = new Worker(new URL(wasmUrl), { type: 'module' });
  worker.postMessage({ type: 'init', mode: 'spawn', wasmUrl, buildHash, fnIdx, argBytes, chanSabs });
  spawnedWorkerChans.set(worker, chanSabs);
  worker.onmessage = function(e) {
    if (e.data && e.data.type === 'exit') spawnedWorkerChans.delete(worker);
  };
  worker.onerror = function(e) {
    const sabs = spawnedWorkerChans.get(worker);
    spawnedWorkerChans.delete(worker);
    console.error('[spawn] child Worker died:', e.message,
      'at', e.filename + ':' + e.lineno,
      e.error && e.error.stack ? '\n' + e.error.stack : '');
    if (sabs) sabs.forEach(sabForceClose);
  };
  worker.onmessageerror = function() {
    const sabs = spawnedWorkerChans.get(worker);
    spawnedWorkerChans.delete(worker);
    if (sabs) sabs.forEach(sabForceClose);
  };
}

// ── Common bridge factory ──────────────────────────────────────────────────

// makeCommonBridge returns the bridge functions shared by all wasm Worker
// hosts. The host merges these with its own specific functions (ext_*, dom_*,
// etc.) to form the complete bridge object passed to WebAssembly.instantiate.
//
// h: the result of makeCoreHelpers(getMem, getXp)
// sabTable: Map<int32, SharedArrayBuffer> - handle table (mutated by channel_create)
// sabSeqRef: { value: int32 } - next handle sequence number (mutable)
// wasmUrlRef: { value: string|null } - set during wasm boot
// buildHashRef: { value: string|null } - set during wasm boot
// spawnedWorkerChans: Map<Worker, SAB[]> - dead-Worker cleanup tracking
export function makeCommonBridge(h, sabTable, sabSeqRef, wasmUrlRef, buildHashRef, spawnedWorkerChans) {
  const { readStr, readBytes, writeBytes, writeI32, cbs, cbdata } = h;

  return {
    // --- runtime context ---
    is_worker_context() { return 1; },

    // --- SAB channels ---
    channel_create(slotSize, slotCount) {
      const sab = sabCreate(slotSize, slotCount);
      const handle = sabSeqRef.value++;
      sabTable.set(handle, sab);
      return handle;
    },
    channel_send(handle, srcPtr, srcLen, srcCap) {
      const sab = sabTable.get(handle);
      if (!sab) throw new Error('channel_send: invalid handle ' + handle);
      // .slice() copies bytes out before the Atomics.wait in sabSend can
      // conflict with wasm advancing the memory pointer during a GC.
      sabSend(sab, readBytes(srcPtr, srcLen).slice());
    },
    channel_recv(handle, dstPtr, dstCap, cap) {
      const sab = sabTable.get(handle);
      if (!sab) return -1;
      const dst = new Uint8Array(dstCap);
      const n = sabRecv(sab, dst);
      if (n > 0) {
        // readBytes returns a writable view into wasm linear memory at dstPtr.
        // set() writes the received bytes directly to that address.
        readBytes(dstPtr, n).set(dst.subarray(0, n));
      }
      return n;
    },
    channel_close(handle) {
      const sab = sabTable.get(handle);
      if (sab) sabClose(sab);
    },

    // --- spawn ---
    spawn_domain(fnIdx, argPtr, argLen, chanHandlesPtr, nChans) {
      if (!wasmUrlRef.value) throw new Error('spawn_domain: wasmUrl not set');
      const chanSabs = [];
      for (let i = 0; i < nChans; i++) {
        // readBytes returns a view; DataView reads the int32 handle at each offset.
        const handleData = readBytes(chanHandlesPtr + i * 4, 4);
        const hh = new DataView(handleData.buffer, handleData.byteOffset, 4).getInt32(0, true);
        const sab = sabTable.get(hh);
        if (sab) chanSabs.push(sab);
      }
      const argBytes = argLen > 0 ? readBytes(argPtr, argLen).slice() : new Uint8Array(0);
      createSpawnedWorker(wasmUrlRef.value, buildHashRef.value, fnIdx, argBytes, chanSabs, spawnedWorkerChans);
    },

    // --- time ---
    timezone_offset_minutes() {
      return new Date().getTimezoneOffset();
    },

    // --- subtle ---
    subtle_random_bytes(ptr, len, cap) {
      crypto.getRandomValues(readBytes(ptr, len));
    },
  };
}
