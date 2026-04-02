// TinyJS Runtime — Channels
// Go channels with buffered/unbuffered semantics, close, and select.

export class Channel {
  constructor(bufferSize = 0) {
    this.bufferSize = bufferSize;
    this.buffer = [];
    this.closed = false;
    this.sendWaiters = []; // [{value, resolve, reject}]
    this.recvWaiters = []; // [{resolve, reject}]
  }

  // Skip cancelled waiters (from resolved selects).
  _shiftRecv() {
    while (this.recvWaiters.length > 0) {
      const w = this.recvWaiters.shift();
      if (!w.cancelled) return w;
    }
    return null;
  }

  _shiftSend() {
    while (this.sendWaiters.length > 0) {
      const w = this.sendWaiters.shift();
      if (!w.cancelled) return w;
    }
    return null;
  }

  // Send a value. Blocks (awaits) if buffer full and no receiver waiting.
  async send(value) {
    if (this.closed) {
      throw new Error('send on closed channel');
    }

    // If a receiver is waiting, hand off directly.
    const waiter = this._shiftRecv();
    if (waiter) {
      waiter.resolve({ value, ok: true });
      return;
    }

    // If buffer has space, enqueue.
    if (this.buffer.length < this.bufferSize) {
      this.buffer.push(value);
      return;
    }

    // Block until a receiver arrives.
    return new Promise((resolve, reject) => {
      this.sendWaiters.push({ value, resolve, reject });
    });
  }

  // Receive a value. Blocks (awaits) if buffer empty and no sender waiting.
  async recv() {
    // If buffer has data, return it. Then unblock a sender if waiting.
    if (this.buffer.length > 0) {
      const value = this.buffer.shift();
      const sender = this._shiftSend();
      if (sender) {
        this.buffer.push(sender.value);
        sender.resolve();
      }
      return { value, ok: true };
    }

    // If a sender is waiting (unbuffered or buffer was empty), take directly.
    const sender = this._shiftSend();
    if (sender) {
      sender.resolve();
      return { value: sender.value, ok: true };
    }

    // If closed and nothing buffered, return zero value.
    if (this.closed) {
      return { value: undefined, ok: false };
    }

    // Block until a sender arrives or channel closes.
    return new Promise((resolve) => {
      this.recvWaiters.push({ resolve });
    });
  }

  // Close the channel.
  close() {
    if (this.closed) {
      throw new Error('close of closed channel');
    }
    this.closed = true;

    // Wake all waiting receivers with zero value.
    let w;
    while ((w = this._shiftRecv())) {
      w.resolve({ value: undefined, ok: false });
    }

    // Panic on waiting senders.
    while ((w = this._shiftSend())) {
      w.reject(new Error('send on closed channel'));
    }
  }

  // Non-blocking try-send. Returns true if sent.
  trySend(value) {
    if (this.closed) throw new Error('send on closed channel');

    const waiter = this._shiftRecv();
    if (waiter) {
      waiter.resolve({ value, ok: true });
      return true;
    }

    if (this.buffer.length < this.bufferSize) {
      this.buffer.push(value);
      return true;
    }

    return false;
  }

  // Non-blocking try-recv. Returns {value, ok, received}.
  tryRecv() {
    if (this.buffer.length > 0) {
      const value = this.buffer.shift();
      const sender = this._shiftSend();
      if (sender) {
        this.buffer.push(sender.value);
        sender.resolve();
      }
      return { value, ok: true, received: true };
    }

    const sender = this._shiftSend();
    if (sender) {
      sender.resolve();
      return { value: sender.value, ok: true, received: true };
    }

    if (this.closed) {
      return { value: undefined, ok: false, received: true };
    }

    return { value: undefined, ok: false, received: false };
  }
}

// Select statement implementation.
// cases: [{ch, dir: 'send'|'recv', value?, id}]
// hasDefault: boolean
// Returns: {id, value, ok}
export async function select(cases, hasDefault) {
  // Shuffle cases for fairness (Go spec requires pseudo-random selection).
  const shuffled = cases.map((c, i) => ({ ...c, origIndex: i }));
  for (let i = shuffled.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1));
    [shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]];
  }

  // Try non-blocking first.
  for (const c of shuffled) {
    if (c.dir === 'send') {
      if (c.ch.trySend(c.value)) {
        return { id: c.id, value: undefined, ok: true };
      }
    } else {
      const result = c.ch.tryRecv();
      if (result.received) {
        return { id: c.id, value: result.value, ok: result.ok };
      }
    }
  }

  // Default case.
  if (hasDefault) {
    return { id: -1, value: undefined, ok: false };
  }

  // Block: race all cases. Use cancel flags instead of splice for O(1) cleanup.
  return new Promise((resolve, reject) => {
    const waiters = [];
    let resolved = false;

    function cancelOthers() {
      for (const w of waiters) w.cancelled = true;
    }

    for (const c of shuffled) {
      if (c.dir === 'recv') {
        const waiter = {
          cancelled: false,
          resolve: (result) => {
            if (resolved) return;
            resolved = true;
            cancelOthers();
            resolve({ id: c.id, value: result.value, ok: result.ok });
          }
        };
        waiters.push(waiter);
        c.ch.recvWaiters.push(waiter);
      } else {
        const waiter = {
          cancelled: false,
          value: c.value,
          resolve: () => {
            if (resolved) return;
            resolved = true;
            cancelOthers();
            resolve({ id: c.id, value: undefined, ok: true });
          },
          reject: (err) => {
            if (resolved) return;
            resolved = true;
            cancelOthers();
            reject(err);
          }
        };
        waiters.push(waiter);
        c.ch.sendWaiters.push(waiter);
      }
    }
  });
}
