// supervisor.mjs - the page's domain hub.
//
// A wasm domain runs in a Worker, and a Worker started by a Worker does not
// begin to run while its creator is parked in Atomics.wait. The page is the
// only realm that is never parked in wasm, so the page starts every domain and
// every domain holds one MessagePort to it. This module is the logic behind
// that port: ids, the tree, the message dispatch and the log queue.
//
// No DOM, no Worker, no page: the host passes in `start` (create a domain),
// `stop` (kill one), `log`, `fault` and `exited`, so the whole protocol can be
// driven from Node with fake ports. See docs/WASM_HUB.md for the contract.
//
// The data plane does not come through here. A spawn channel is a ring the
// spawning domain created; its handles ride in the boot payload, which the hub
// forwards without reading.

export function createHub(opts = {}) {
  const startDomain = opts.start;
  const stopDomain = opts.stop || (() => {});
  const logLine = opts.log || (() => {});
  const fault = opts.fault || (() => {});
  const exited = opts.exited || (() => {});
  const now = opts.now || (() => 0);
  const schedule = opts.schedule || null;

  const nodes = new Map();
  const roots = [];
  let rootSeq = 0;
  const stats = {
    spawned: 0, exited: 0, panics: 0, rebinds: 0,
    refusedSpawn: 0, portMismatch: 0, parentMismatch: 0, unknownKind: 0,
    unknownExit: 0, unknownPanic: 0, unknownHeartbeat: 0, unknownRebind: 0,
    unattributedLog: 0, drained: 0,
  };

  let queue = [];
  let scheduled = false;
  // Per-domain line assembly. Two domains print concurrently, and their writes
  // arrive in pieces: sharing one buffer would splice "b got" and "root got 10"
  // into a line neither program wrote. The hub is the only place that sees all
  // the ids, so it is where a line becomes whole.
  const partials = new Map();   // "<id>|<fd>" -> unterminated text

  function enqueue(entry) {
    queue.push(entry);
    if (!scheduled && schedule) {
      scheduled = true;
      schedule(() => { scheduled = false; drain(); });
    }
  }

  // drain hands queued log lines to the page, in arrival order. Bounded so one
  // burst cannot hold the page's frame for longer than its batch.
  function drain(max = 256) {
    const n = Math.min(max, queue.length);
    for (let i = 0; i < n; i++) {
      logLine(queue[i]);
      stats.drained++;
    }
    if (n > 0) queue = queue.slice(n);
    return queue.length;
  }

  function emitLines(id, fd, text) {
    const key = id + '|' + fd;
    let buf = (partials.get(key) || '') + text;
    let nl;
    while ((nl = buf.indexOf('\n')) >= 0) {
      enqueue({ id, fd, text: buf.slice(0, nl + 1) });
      buf = buf.slice(nl + 1);
    }
    if (buf.length > 0) partials.set(key, buf);
    else partials.delete(key);
  }

  // A domain that is done writing must not lose its last, unterminated line.
  function flushPartials(id) {
    for (const key of [...partials.keys()]) {
      const sep = key.indexOf('|');
      if (key.slice(0, sep) !== id) continue;
      const text = partials.get(key);
      partials.delete(key);
      enqueue({ id, fd: Number(key.slice(sep + 1)), text });
    }
  }

  function nodeOf(id) {
    if (typeof id !== 'string') return null;
    return nodes.get(id) || null;
  }

  function childrenOf(id) {
    const out = [];
    for (const n of nodes.values()) if (n.parent === id) out.push(n.id);
    return out;
  }

  // retire removes one node. `adopt` is the clean-exit rule: its children
  // survive, re-parented to its parent and told so; without it the subtree is
  // being torn down and the children are retired separately.
  function retire(node, adopt) {
    if (!node || node.gone) return;
    node.gone = true;
    // The host decides what stopping means: a page host keeps a root's Worker
    // alive for callbacks and terminates every other domain.
    stopDomain(node.handle, node);
    flushPartials(node.id);
    if (node.root) {
      const at = roots.indexOf(node.id);
      if (at >= 0) roots.splice(at, 1);
    }
    const kids = childrenOf(node.id);
    const parent = nodes.get(node.parent) || null;
    if (parent) parent.children = parent.children.filter((c) => c !== node.id);
    if (adopt) {
      for (const cid of kids) {
        const c = nodes.get(cid);
        if (!c) continue;
        // A root's exit stops its tree, so the only adoption that reaches here
        // is an intermediate's: the children are grafted onto its parent.
        c.parent = node.parent;
        if (parent) parent.children.push(cid);
        stats.rebinds++;
        try { c.port && c.port.postMessage({ kind: 'rebind', parent: node.parent }); } catch (e) { /* the domain is gone */ }
      }
    }
    nodes.delete(node.id);
    stats.exited++;
    exited(node.id, node.root === true);
  }

  function stopSubtree(id) {
    const root = nodeOf(id);
    if (!root) return;
    const kids = childrenOf(id).slice();
    for (const cid of kids) stopSubtree(cid);
    retire(root, false);
  }

  function stopAll() {
    for (const id of [...roots]) stopSubtree(id);
    for (const id of [...nodes.keys()]) retire(nodes.get(id), false);
    roots.length = 0;
  }

  function attach({ id, parent, port, handle, root }) {
    if (typeof id !== 'string' || id.length === 0) throw new Error('supervisor.attach: id must be a non-empty string');
    if (nodes.has(id)) throw new Error('supervisor.attach: duplicate id ' + id);
    const node = {
      id, parent: parent || '0', port, handle: handle || null,
      children: [], spawned: 0, lastSeen: now(), gone: false, root: root === true,
    };
    nodes.set(id, node);
    const p = nodes.get(node.parent);
    if (p) p.children.push(id);
    if (node.root) roots.push(id);
    return node;
  }

  // attachRoot gives a module's root domain the next root id. Ids are never
  // reused, so the counter is monotonic even after a root exits.
  function attachRoot({ port, handle }) {
    const id = String(++rootSeq);
    attach({ id, parent: '0', port, handle, root: true });
    return id;
  }

  // A refused spawn answers on the port it arrived on: that port belongs to the
  // sender whether or not its id is in the tree, and the sender must learn that
  // no domain was started for it.
  function refuse(port, id, reason) {
    stats.refusedSpawn++;
    const p = port || null;
    if (p) {
      try { p.postMessage({ kind: 'refused', id, reason }); } catch (e) { /* gone */ }
    }
  }

  function onSpawn(port, msg) {
    const node = nodeOf(msg.id);
    if (!node) { refuse(port, typeof msg.id === 'string' ? msg.id : '', 'unknown-sender'); return; }
    if (node.port !== port) { stats.portMismatch++; refuse(port, node.id, 'port-mismatch'); return; }
    if (typeof msg.parent === 'string' && msg.parent !== node.parent) stats.parentMismatch++;
    if (!startDomain) { refuse(port, node.id, 'no-start'); return; }
    node.spawned++;
    const childId = node.id + '.' + node.spawned;
    const boot = {
      kind: 'entry',
      id: childId,
      parent: node.id,
      wasm: msg.wasm,
      fnIdx: msg.fnIdx,
      args: msg.args,
      chans: msg.chans,
      breaker: msg.breaker,
    };
    const started = startDomain(boot);
    if (!started || !started.port) throw new Error('supervisor: start() must return { handle, port }');
    attach({ id: childId, parent: node.id, port: started.port, handle: started.handle, root: false });
    stats.spawned++;
  }

  // handle is the one entry point for a message that arrived on a domain's
  // port. Control and logs are dispatched separately: a stale id may still
  // print its log line, but it may not drive the tree.
  function handle(port, msg) {
    if (!msg || typeof msg.kind !== 'string') { stats.unknownKind++; return; }
    switch (msg.kind) {
      case 'log': {
        const node = nodeOf(msg.id);
        if (!node) stats.unattributedLog++;
        else if (node.port !== port) stats.portMismatch++;
        emitLines(node ? node.id : '?', msg.fd === 2 ? 2 : 1, String(msg.text == null ? '' : msg.text));
        return;
      }
      case 'spawn': onSpawn(port, msg); return;
      case 'exit': {
        const node = nodeOf(msg.id);
        if (!node) { stats.unknownExit++; return; }
        // Every clean exit adopts. A root's exit is not a process exit on wasm:
        // the page keeps the root Worker for callbacks, and its children are
        // still live capabilities the app holds breakers for. A page teardown
        // (stopAll) is what ends a tree.
        retire(node, true);
        return;
      }
      case 'panic': {
        const node = nodeOf(msg.id);
        if (!node) { stats.unknownPanic++; return; }
        stats.panics++;
        // Native parity: a panic aborts the process, so the page stops every
        // domain on it. The tree is empty afterwards; the fault is the story.
        stopAll();
        fault({ id: node.id, text: String(msg.text == null ? '' : msg.text) });
        return;
      }
      case 'rebind':
        // Hub -> domain only. A domain asking to move itself is a bug in the
        // sender, not an instruction to follow.
        stats.unknownRebind++;
        return;
      case 'heartbeat': {
        const node = nodeOf(msg.id);
        if (!node) { stats.unknownHeartbeat++; return; }
        node.lastSeen = now();
        return;
      }
      default:
        stats.unknownKind++;
    }
  }

  function tree() {
    const out = {};
    for (const n of nodes.values()) {
      out[n.id] = { parent: n.parent, children: n.children.slice(), root: n.root === true };
    }
    return out;
  }

  // panic is the host's entry point for a Worker that died without posting:
  // the page saw its error event, not a message from the domain.
  function panic(id, text) {
    handle(null, { kind: 'panic', id, text });
  }

  return { attach, attachRoot, handle, panic, drain, tree, stopAll, stopSubtree, stats: () => stats, pending: () => queue.length };
}
