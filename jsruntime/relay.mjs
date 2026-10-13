// relay.mjs — the page <-> Worker DOM boundary.
//
// A Moxie module runs in a Worker and the document lives on the page, so the
// module cannot touch the DOM directly. It asks, and waits: the request goes
// over a MessagePort, the page performs the operation from dom-ops.mjs and
// answers through a SharedArrayBuffer, and the Worker's Atomics.wait on the
// gate is what makes the call synchronous from inside wasm. The page thread
// cannot use Atomics.wait at all, which is exactly why the module is in the
// Worker and the page is the server.
//
// Every request is answered, including the ones with no value, so DOM order is
// exact: a call that returns has already happened on the page. A failed
// operation does not come back as a zero the module might mistake for a real
// answer - the page puts the message in the buffer and the Worker throws, so a
// broken DOM call kills the domain loudly instead of corrupting it quietly.
import { domOps, methodName, INT, STR, I64 } from './dom-ops.mjs';

export const RELAY = 'moxie.dom.relay';

// Layout of the shared buffer: four i32 words of header, then UTF-8 payload.
//   i32[0] gate    0 while the page is working, 1 on success, -1 on failure
//   i32[1] int     an INT answer
//   i32[2] len     payload length in bytes
//   i32[3] seq     the sequence number of the request being answered
const HEADER = 16;
const PAYLOAD = 1 << 20;
const WAIT_MS = 30000;

const encoder = new TextEncoder();
const decoder = new TextDecoder();

// makeDomProxy builds the object the module's bridge calls instead of dom.mjs
// when the document is one hop away. Its methods are named after dom.mjs's
// exports, so the bridge in host.mjs has one shape whether the caller is on
// the page or in a Worker.
export function makeDomProxy(port) {
  const proxy = {};
  const client = port ? new RelayClient(port) : null;
  for (const op of Object.keys(domOps)) {
    proxy[methodName(op)] = (...args) => {
      if (!client) {
        // Only the root domain holds the page connection. A spawned domain
        // reaches the DOM by asking the page directly, which needs the page to
        // hand it a port; until then this is loud rather than silent.
        throw new Error(`dom.${op}: this domain has no DOM relay (only the root domain is connected to the page)`);
      }
      if (!domOps[op].run) throw new Error(`dom.${op}: ${domOps[op].why}`);
      return client.call(op, args, domOps[op].kind);
    };
  }
  return proxy;
}

// RelayClient is one domain's end of the boundary. The buffer is allocated on
// first use, not at import: only a domain that touches the DOM needs it, and
// SharedArrayBuffer does not exist outside a cross-origin isolated page.
class RelayClient {
  constructor(port) {
    this.port = port;
    this.seq = 0;
    this.sab = null;
    this.i32 = null;
    this.payload = null;
  }

  call(op, args, kind) {
    if (!this.sab) {
      this.sab = new SharedArrayBuffer(HEADER + PAYLOAD);
      this.i32 = new Int32Array(this.sab);
      this.payload = new Uint8Array(this.sab, HEADER);
    }
    const i32 = this.i32;
    const mine = ++this.seq;
    i32[1] = 0;
    i32[2] = 0;
    i32[0] = 0; // clear the gate before the request can be seen
    this.port.postMessage({ relay: RELAY, seq: mine, op, args, sab: this.sab });
    const woke = Atomics.wait(i32, 0, 0, WAIT_MS);
    if (woke === 'timed-out') {
      throw new Error(`dom.${op}: the page did not answer within ${WAIT_MS}ms`);
    }
    if (i32[3] !== mine) {
      throw new Error(`dom.${op}: relay answer ${i32[3]} arrived for request ${mine}`);
    }
    if (i32[0] < 0) throw new Error('dom.' + op + ': ' + this.decode(i32[2]));
    if (kind === INT) return i32[1];
    if (kind === STR) return this.decode(i32[2]);
    if (kind === I64) return BigInt(this.decode(i32[2]));
    return undefined; // VOID
  }

  decode(len) {
    if (len <= 0) return '';
    // Firefox refuses to decode a view over a SharedArrayBuffer, so copy first.
    const copy = new Uint8Array(len);
    copy.set(this.payload.subarray(0, len));
    return decoder.decode(copy);
  }
}

// serveRelay answers the module's DOM requests on the page. host.mjs's
// startDomain installs one per root domain, so a page needs no setup of its
// own to host an app.
export function serveRelay(port) {
  // A callback the page produces later - an event, a timer, a fetch reply -
  // travels back over the same port and is dispatched into the module's exports
  // by worker-host. The ids are the module's own web/callback handles, so the
  // code that registered the callback is the code that runs.
  const cb = {
    void: (id) => port.postMessage({ cb: 'void', id }),
    str: (id, s) => port.postMessage({ cb: 'str', id, s }),
    bool: (id, b) => port.postMessage({ cb: 'bool', id, b }),
    i6: (id, a) => port.postMessage({ cb: 'i6', id, a }),
    ss: (id, s, t) => port.postMessage({ cb: 'ss', id, s, t }),
  };
  port.addEventListener('message', (e) => {
    const m = e.data;
    if (!m || m.relay !== RELAY) return;
    const gate = new Int32Array(m.sab);
    const out = new Uint8Array(m.sab, HEADER);
    const answer = (state, str) => {
      let b = str === undefined ? null : encoder.encode(str);
      if (b && b.length > PAYLOAD) {
        // Truncating an answer would hand the module a string the page does
        // not have, so replace it with a message that says so.
        state = -1;
        b = encoder.encode(`answer exceeds the ${PAYLOAD}-byte relay buffer`);
      }
      if (b) out.set(b);
      gate[2] = b ? b.length : 0;
      gate[3] = m.seq;
      Atomics.store(gate, 0, state);
      Atomics.notify(gate, 0, 1);
    };
    const op = domOps[m.op];
    // An unknown op means the two halves of the relay disagree about the op
    // set; answering with an error makes that visible instead of hanging the
    // Worker on a request nobody will serve.
    if (!op) return answer(-1, `no such operation (${m.op})`);
    if (!op.run) return answer(-1, op.why);
    try {
      const v = op.run(m.args, cb);
      if (op.kind === INT) gate[1] = v;
      if (op.kind === STR || op.kind === I64) return answer(1, String(v));
      answer(1, undefined);
    } catch (err) {
      answer(-1, String((err && err.message) || err));
    }
  });
  port.start();
}
