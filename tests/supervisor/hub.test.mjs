// The hub protocol, with no browser and no Worker: fake ports in, messages
// out. docs/WASM_HUB.md is the contract these tests assert.
import test from 'node:test';
import assert from 'node:assert/strict';
import { createHub } from '../../jsruntime/supervisor.mjs';

function fakePort() {
  const sent = [];
  return { sent, postMessage: (m) => sent.push(m), start() {} };
}

function makeHub(opts = {}) {
  const state = { started: [], stopped: [], logs: [], faults: [], exits: [], clock: 1000 };
  const hub = createHub({
    queueMax: opts.queueMax,
    start: (boot) => {
      const port = fakePort();
      const handle = 'w-' + boot.id;
      state.started.push({ boot, port, handle });
      return { handle, port };
    },
    stop: (h, node) => state.stopped.push({ handle: h, root: !!(node && node.root) }),
    log: (e) => state.logs.push(e),
    fault: (f) => state.faults.push(f),
    exited: (id, root) => state.exits.push({ id, root }),
    now: () => state.clock,
  });
  return { hub, ...state };
}

function bootOf(state, id) {
  return state.started.find((s) => s.boot.id === id);
}

function handles(state) {
  return state.stopped.map((s) => s.handle);
}

function spawn(hub, port, id, parent = '0') {
  hub.handle(port, { kind: 'spawn', id, parent, wasm: '/app.wasm', fnIdx: 0, args: new Uint8Array(0), chans: [], breaker: null });
}

test('lineage ids and parents are assigned by the hub', () => {
  const { hub, started } = makeHub();
  const root = fakePort();
  assert.equal(hub.attachRoot({ port: root, handle: 'w-1' }), '1');
  spawn(hub, root, '1');
  assert.equal(started[0].boot.id, '1.1');
  assert.equal(started[0].boot.parent, '1');
  spawn(hub, started[0].port, '1.1', '1');
  assert.equal(started[1].boot.id, '1.1.1');
  assert.equal(started[1].boot.parent, '1.1');
  const t = hub.tree();
  assert.equal(t['1.1.1'].parent, '1.1');
  assert.deepEqual(t['1'].children, ['1.1']);
});

test('the boot payload crosses untouched', () => {
  const { hub, started } = makeHub();
  const root = fakePort();
  hub.attachRoot({ port: root, handle: 'w-1' });
  const args = new Uint8Array([1, 2, 3]);
  const chans = [{ handle: 7, sab: { fake: true } }];
  hub.handle(root, { kind: 'spawn', id: '1', parent: '0', wasm: '/x.wasm', fnIdx: 42, args, chans, breaker: { handle: 9 } });
  const b = started[0].boot;
  assert.equal(b.fnIdx, 42);
  assert.equal(b.wasm, '/x.wasm');
  assert.equal(b.args, args);
  assert.equal(b.chans, chans);
  assert.deepEqual(b.breaker, { handle: 9 });
});

test('a spawn from an unknown id is refused, not started', () => {
  const { hub, started } = makeHub();
  const ghost = fakePort();
  spawn(hub, ghost, '9.9', '9');
  assert.equal(started.length, 0);
  assert.equal(hub.stats().refusedSpawn, 1);
  assert.equal(ghost.sent[0].kind, 'refused');
  assert.equal(ghost.sent[0].reason, 'unknown-sender');
});

test('a spawn on the wrong port is refused', () => {
  const { hub, started } = makeHub();
  const root = fakePort();
  hub.attachRoot({ port: root, handle: 'w-1' });
  const other = fakePort();
  spawn(hub, other, '1', '0');
  assert.equal(started.length, 0);
  assert.equal(hub.stats().portMismatch, 1);
  assert.equal(other.sent[0].reason, 'port-mismatch');
});

test('logs from an unknown id are printed as ? and counted, never dropped', () => {
  const { hub, logs } = makeHub();
  hub.handle(fakePort(), { kind: 'log', id: '2.7', fd: 2, text: 'ghost line\n' });
  assert.equal(hub.pending(), 1);
  hub.drain();
  assert.deepEqual(logs, [{ id: '?', fd: 2, text: 'ghost line\n' }]);
  assert.equal(hub.stats().unattributedLog, 1);
});

test('the orphan invariant: A exits, B keeps logging and exits cleanly', () => {
  const { hub, started, stopped, logs } = makeHub();
  const root = fakePort();
  hub.attachRoot({ port: root, handle: 'w-1' });
  spawn(hub, root, '1');                       // A = 1.1
  const a = bootOf({ started }, '1.1');
  spawn(hub, a.port, '1.1', '1');              // B = 1.1.1
  const b = bootOf({ started }, '1.1.1');

  hub.handle(a.port, { kind: 'exit', id: '1.1' });
  assert.ok(handles({ stopped }).includes('w-1.1'), 'A was stopped');
  assert.equal(hub.tree()['1.1'], undefined);
  assert.equal(hub.tree()['1.1.1'].parent, '1', 'B was adopted by the root');
  assert.ok(b.port.sent.some((m) => m.kind === 'rebind' && m.parent === '1'), 'B was told');

  hub.handle(b.port, { kind: 'log', id: '1.1.1', fd: 1, text: 'b alive\n' });
  hub.drain();
  assert.deepEqual(logs.at(-1), { id: '1.1.1', fd: 1, text: 'b alive\n' });

  hub.handle(b.port, { kind: 'exit', id: '1.1.1' });
  assert.equal(hub.tree()['1.1.1'], undefined);
  assert.equal(hub.stats().exited, 2);
});

test('a parent parked in a receive does not block its child', () => {
  const { hub, started, logs } = makeHub();
  const root = fakePort();
  hub.attachRoot({ port: root, handle: 'w-1' });
  spawn(hub, root, '1');                       // A = 1.1
  const a = bootOf({ started }, '1.1');
  // A sends one spawn and then nothing else - it is parked inside the wasm
  // receive its child will wake. The hub must start the child and carry its
  // messages without A pumping anything.
  spawn(hub, a.port, '1.1', '1');              // B = 1.1.1
  const b = bootOf({ started }, '1.1.1');
  hub.handle(b.port, { kind: 'log', id: '1.1.1', fd: 1, text: 'b up\n' });
  hub.drain();
  assert.equal(started.length, 2);
  assert.deepEqual(logs.at(-1), { id: '1.1.1', fd: 1, text: 'b up\n' });
  assert.equal(a.port.sent.length, 0, 'nothing was required of the parked parent');
});

test('a panic stops every domain and reports one fault', () => {
  const { hub, started, stopped, faults } = makeHub();
  const root = fakePort();
  hub.attachRoot({ port: root, handle: 'w-1' });
  spawn(hub, root, '1');
  const a = bootOf({ started }, '1.1');
  spawn(hub, a.port, '1.1', '1');
  const b = bootOf({ started }, '1.1.1');
  hub.handle(b.port, { kind: 'panic', id: '1.1.1', text: 'boom' });
  assert.equal(faults.length, 1);
  assert.equal(faults[0].id, '1.1.1');
  assert.deepEqual(handles({ stopped }).sort(), ['w-1', 'w-1.1', 'w-1.1.1']);
  assert.deepEqual(hub.tree(), {});
});

test('a root exit keeps its live children; a new root never reuses an id', () => {
  const { hub, started, stopped } = makeHub();
  const one = fakePort();
  const two = fakePort();
  const oneId = hub.attachRoot({ port: one, handle: 'w-1' });
  const twoId = hub.attachRoot({ port: two, handle: 'w-2' });
  assert.deepEqual([oneId, twoId], ['1', '2']);
  spawn(hub, one, '1');
  spawn(hub, two, '2');
  hub.handle(one, { kind: 'exit', id: '1' });
  // The page keeps the root Worker for callbacks, so a clean root exit is not a
  // tree kill: its child is adopted by the hub and keeps running.
  assert.equal(hub.tree()['1'], undefined);
  assert.equal(hub.tree()['1.1'].parent, '0');
  assert.ok(!handles({ stopped }).includes('w-1.1'));
  const three = fakePort();
  assert.equal(hub.attachRoot({ port: three, handle: 'w-3' }), '3');
  hub.stopAll();
  // The hub ASKS for every node to stop, and tells the host whether it is a
  // root: a page host keeps the root's Worker alive for callbacks and stops
  // every other domain.
  assert.deepEqual(stopped.find((s) => s.handle === 'w-1'), { handle: 'w-1', root: true });
  assert.deepEqual(stopped.find((s) => s.handle === 'w-2'), { handle: 'w-2', root: true });
  assert.deepEqual(stopped.find((s) => s.handle === 'w-3'), { handle: 'w-3', root: true });
  assert.deepEqual(stopped.filter((s) => s.handle === 'w-1.1'), [{ handle: 'w-1.1', root: false }]);
  assert.deepEqual(handles({ stopped }).sort(), ['w-1', 'w-1.1', 'w-2', 'w-2.1', 'w-3']);
});

test('heartbeat updates lastSeen; an unknown heartbeat is counted only', () => {
  const { hub } = makeHub();
  const root = fakePort();
  hub.attachRoot({ port: root, handle: 'w-1' });
  const before = hub.tree()['1'];
  hub.handle(root, { kind: 'heartbeat', id: '1' });
  assert.deepEqual(hub.tree()['1'], before);
  hub.handle(root, { kind: 'heartbeat', id: '7.7' });
  assert.equal(hub.stats().unknownHeartbeat, 1);
});

test('a rebind addressed to the hub is a protocol error, not an instruction', () => {
  const { hub } = makeHub();
  const root = fakePort();
  hub.attachRoot({ port: root, handle: 'w-1' });
  hub.handle(root, { kind: 'rebind', parent: '0' });
  assert.equal(hub.tree()['1'].parent, '0');
  assert.equal(hub.stats().unknownRebind, 1);
});

test('lines are assembled per domain, so two domains cannot splice one another', () => {
  const { hub, started, logs } = makeHub();
  const root = fakePort();
  hub.attachRoot({ port: root, handle: 'w-1' });
  spawn(hub, root, '1');                       // child 1.1
  const child = started[0].port;
  // Interleave the two domains' pieces; each line must stay whole.
  hub.handle(root, { kind: 'log', id: '1', fd: 1, text: 'root got' });
  hub.handle(child, { kind: 'log', id: '1.1', fd: 1, text: 'b got' });
  assert.equal(hub.drain(), 0, 'no line is complete yet');
  hub.handle(root, { kind: 'log', id: '1', fd: 1, text: ' 10\n' });
  hub.handle(child, { kind: 'log', id: '1.1', fd: 1, text: ' 7\n' });
  hub.drain();
  assert.deepEqual(logs.slice(-2), [
    { id: '1', fd: 1, text: 'root got 10\n' },
    { id: '1.1', fd: 1, text: 'b got 7\n' },
  ]);
  // A domain that exits with an unterminated line still gets it printed.
  hub.handle(child, { kind: 'log', id: '1.1', fd: 1, text: 'unterminated' });
  hub.handle(child, { kind: 'exit', id: '1.1' });
  hub.drain();
  assert.deepEqual(logs.at(-1), { id: '1.1', fd: 1, text: 'unterminated' });
});

test('an overflowing queue drops with a per-domain count, reported at exit', () => {
  const { hub, logs } = makeHub({ queueMax: 3 });
  const root = fakePort();
  hub.attachRoot({ port: root, handle: 'w-1' });
  for (let i = 0; i < 5; i++) hub.handle(root, { kind: 'log', id: '1', fd: 1, text: 'line ' + i + '\n' });
  assert.equal(hub.pending(), 3);
  assert.equal(hub.stats().dropped, 2);
  hub.handle(root, { kind: 'exit', id: '1' });
  hub.drain();
  // The summary must survive the full queue: a lost drop report would make
  // "no output" ambiguous.
  assert.ok(logs.some((l) => l.id === '1' && l.fd === 2 && l.text.includes('2 log lines dropped for 1')),
    JSON.stringify(logs));
});

test('the log queue drains in order and in bounded batches', () => {
  const { hub, logs } = makeHub();
  const root = fakePort();
  hub.attachRoot({ port: root, handle: 'w-1' });
  for (let i = 0; i < 300; i++) hub.handle(root, { kind: 'log', id: '1', fd: 1, text: 'line ' + i + '\n' });
  assert.equal(hub.pending(), 300);
  assert.equal(hub.drain(256), 44);
  assert.equal(logs.length, 256);
  assert.equal(logs[0].text, 'line 0\n');
  assert.equal(logs[255].text, 'line 255\n');
  hub.drain();
  assert.equal(logs.length, 300);
  assert.equal(logs[299].text, 'line 299\n');
  assert.equal(hub.stats().drained, 300);
});
