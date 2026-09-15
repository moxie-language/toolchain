// sab-channel.mjs — SPSC ring buffer channel over SharedArrayBuffer.
//
// SAB layout (bytes):
//   [0..3]   write_idx  u32  (sender advances; receiver waits here)
//   [4..7]   read_idx   u32  (receiver advances; sender waits here)
//   [8..11]  slot_size  u32  const, power-of-two
//   [12..15] slot_count u32  const, power-of-two
//   [16..19] flags      u32  bit0=closed
//   [20..31] reserved
//   [32..]   slots      slot_count * slot_size bytes
//
// Slot layout (slot_size bytes):
//   [0]     is_final   u8
//   [1..3]  reserved
//   [4..7]  total_len  u32 LE  (total message bytes; only in first slot; 0 in subsequent)
//   [8..11] chunk_len  u32 LE  (payload bytes in this slot)
//   [12..]  payload    slot_size - 12 bytes
//
// SPSC: one sender, one receiver. No CAS. slot_count must be power-of-two.

const HDR = 32;      // SAB header bytes before slots
const SLOT_HDR = 12; // per-slot header bytes

export function create(slotSize, slotCount) {
  if ((slotSize & (slotSize - 1)) !== 0) throw new Error('slot_size must be power-of-two');
  if ((slotCount & (slotCount - 1)) !== 0) throw new Error('slot_count must be power-of-two');
  if (slotSize < 64) throw new Error('slot_size minimum 64');
  const sab = new SharedArrayBuffer(HDR + slotCount * slotSize);
  // Int32Array, not Uint32Array: the header is driven by Atomics, and
  // Atomics.wait/notify only accept an Int32Array or BigInt64Array view. The
  // bit patterns are identical, and every value the header holds is small and
  // positive, so the signed view is equivalent here.
  const u32 = new Int32Array(sab);
  u32[2] = slotSize;
  u32[3] = slotCount;
  return sab;
}

// send blocks until a slot is free, then writes bytes as chunked slots.
// Throws if the channel is closed or bytes.length exceeds channel capacity.
export function send(sab, bytes) {
  // Int32Array, not Uint32Array: the header is driven by Atomics, and
  // Atomics.wait/notify only accept an Int32Array or BigInt64Array view. The
  // bit patterns are identical, and every value the header holds is small and
  // positive, so the signed view is equivalent here.
  const u32 = new Int32Array(sab);
  const slotSize = u32[2];
  const slotCount = u32[3];
  const payload_capacity = slotSize - SLOT_HDR;
  const max_msg = (slotCount - 1) * payload_capacity;
  if (bytes.length > max_msg) {
    throw new Error('channel_send: message size ' + bytes.length + ' exceeds channel capacity ' + max_msg);
  }

  const total_len = bytes.length;
  let offset = 0;
  let first = true;

  while (offset <= total_len) {
    const chunk_len = Math.min(total_len - offset, payload_capacity);
    const is_final = (offset + chunk_len >= total_len) ? 1 : 0;

    // Wait for a free slot: write_idx - read_idx < slotCount.
    // Re-check closed flag after each wakeup so a dead receiver unblocks the sender.
    let wi = Atomics.load(u32, 0);
    while (wi - Atomics.load(u32, 1) >= slotCount) {
      Atomics.wait(u32, 1, Atomics.load(u32, 1));
      wi = Atomics.load(u32, 0);
      if (Atomics.load(u32, 4) & 1) throw new Error('channel_send: send on closed channel');
    }
    if (Atomics.load(u32, 4) & 1) throw new Error('channel_send: send on closed channel');

    const slot_idx = wi & (slotCount - 1);
    const base = HDR + slot_idx * slotSize;
    const slot = new DataView(sab, base, slotSize);
    const u8 = new Uint8Array(sab, base, slotSize);

    slot.setUint8(0, is_final);
    slot.setUint8(1, 0); slot.setUint8(2, 0); slot.setUint8(3, 0);
    slot.setUint32(4, first ? total_len : 0, true);
    slot.setUint32(8, chunk_len, true);
    if (chunk_len > 0) u8.set(bytes.subarray(offset, offset + chunk_len), SLOT_HDR);

    Atomics.store(u32, 0, wi + 1);
    Atomics.notify(u32, 0, 1);

    offset += chunk_len;
    first = false;
    if (is_final) break;
  }
}

// recv blocks until a message is available.
// Returns byte count (0 for zero-byte message), or -1 if channel is closed.
// dstBuf must be at least (slotCount - 1) * (slotSize - SLOT_HDR) bytes.
export function recv(sab, dstBuf) {
  // Int32Array, not Uint32Array: the header is driven by Atomics, and
  // Atomics.wait/notify only accept an Int32Array or BigInt64Array view. The
  // bit patterns are identical, and every value the header holds is small and
  // positive, so the signed view is equivalent here.
  const u32 = new Int32Array(sab);
  const slotSize = u32[2];
  const slotCount = u32[3];
  const payload_capacity = slotSize - SLOT_HDR;

  let total_len = -1;
  let dst_offset = 0;
  let awaiting_first = true;

  for (;;) {
    // Wait for a slot. Re-check closed flag after each wakeup so a dead sender
    // (forceClose) unblocks the receiver even with an empty ring.
    let ri = Atomics.load(u32, 1);
    while (Atomics.load(u32, 0) === ri) {
      Atomics.wait(u32, 0, ri);
      ri = Atomics.load(u32, 1);
      if (Atomics.load(u32, 4) & 1) return -1; // closed while waiting
    }
    // Also check before reading: forceClose may have set the flag without
    // writing a sentinel (if the ring was full).
    if (awaiting_first && ri === Atomics.load(u32, 0) && (Atomics.load(u32, 4) & 1)) return -1;

    const slot_idx = ri & (slotCount - 1);
    const base = HDR + slot_idx * slotSize;
    const slot = new DataView(sab, base, slotSize);
    const u8 = new Uint8Array(sab, base, slotSize);

    const is_final = slot.getUint8(0);
    const slot_total_len = slot.getUint32(4, true);
    const chunk_len = slot.getUint32(8, true);

    // Advance read index before processing (allows sender to reuse slot).
    Atomics.store(u32, 1, ri + 1);
    Atomics.notify(u32, 1, 1);

    if (awaiting_first) {
      if (slot_total_len === 0) {
        if (Atomics.load(u32, 4) & 1) return -1; // closed sentinel
        return 0; // zero-byte send
      }
      if (slot_total_len > dstBuf.length) {
        throw new Error('channel_recv: message size ' + slot_total_len + ' exceeds receiver buffer ' + dstBuf.length);
      }
      total_len = slot_total_len;
      awaiting_first = false;
    }

    if (chunk_len > 0) {
      dstBuf.set(u8.subarray(SLOT_HDR, SLOT_HDR + chunk_len), dst_offset);
      dst_offset += chunk_len;
    }

    if (is_final) return total_len;
  }
}

// close sets the closed flag, writes a sentinel zero-len slot, notifies receiver.
// Use when the sender side closes cleanly (knows the ring is not full).
export function close(sab) {
  // Int32Array, not Uint32Array: the header is driven by Atomics, and
  // Atomics.wait/notify only accept an Int32Array or BigInt64Array view. The
  // bit patterns are identical, and every value the header holds is small and
  // positive, so the signed view is equivalent here.
  const u32 = new Int32Array(sab);
  const slotCount = u32[3];

  Atomics.or(u32, 4, 1);

  // Write sentinel slot (blocks if ring is full — clean close path).
  let wi = Atomics.load(u32, 0);
  while (wi - Atomics.load(u32, 1) >= slotCount) {
    Atomics.wait(u32, 1, Atomics.load(u32, 1));
    wi = Atomics.load(u32, 0);
  }
  const slotSize = u32[2];
  const slot_idx = wi & (slotCount - 1);
  const base = HDR + slot_idx * slotSize;
  const slot = new DataView(sab, base, slotSize);
  slot.setUint8(0, 1);       // is_final
  slot.setUint32(4, 0, true); // total_len = 0
  slot.setUint32(8, 0, true); // chunk_len = 0
  Atomics.store(u32, 0, wi + 1);
  Atomics.notify(u32, 0, 1);
}

// forceClose sets the closed flag and wakes all blocked send/recv waiters
// without writing a sentinel slot. Used when a Worker dies unexpectedly.
// No role information needed: flag + notify both ends is sufficient.
//   - Blocked recv: wakes in wait loop, sees bit0, returns -1.
//   - Blocked send: wakes in wait loop, sees bit0, throws "send on closed channel".
export function forceClose(sab) {
  // Int32Array, not Uint32Array: the header is driven by Atomics, and
  // Atomics.wait/notify only accept an Int32Array or BigInt64Array view. The
  // bit patterns are identical, and every value the header holds is small and
  // positive, so the signed view is equivalent here.
  const u32 = new Int32Array(sab);
  Atomics.or(u32, 4, 1);
  Atomics.notify(u32, 0, Atomics.MAX_VALUE); // wake blocked receivers
  Atomics.notify(u32, 1, Atomics.MAX_VALUE); // wake blocked senders
}
