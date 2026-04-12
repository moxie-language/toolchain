// MoxieJS Runtime — Goroutine Scheduler
// Cooperative async/await scheduler with microtask queue.

const runQueue = [];
let running = false;
let goroutineId = 0;

// Error tracking — last N goroutine crashes, accessible via onError callback.
const _errorLog = [];
const _maxErrors = 32;
let _onError = null;

class Goroutine {
  constructor(id, fn, caller) {
    this.id = id;
    this.fn = fn;
    this.caller = caller || '';
    this.done = false;
    this.error = null;
  }
}

// Register a callback for goroutine errors: fn(goroutineId, callerInfo, error).
export function onError(fn) {
  _onError = fn;
}

// Return recent errors as [{id, caller, error, time}].
export function errors() {
  return _errorLog.slice();
}

function recordError(g, e) {
  const entry = { id: g.id, caller: g.caller, error: e, time: Date.now() };
  _errorLog.push(entry);
  if (_errorLog.length > _maxErrors) _errorLog.shift();

  if (typeof e === 'object' && e !== null && e.name === 'GoPanic') {
    console.error(`goroutine ${g.id} [${g.caller}]: panic: ${e.message}`);
    if (e.stack) console.error(e.stack);
  } else {
    console.error(`goroutine ${g.id} [${g.caller}]: unhandled error:`, e);
  }

  if (_onError) {
    try { _onError(g.id, g.caller, e); } catch (_) {}
  }
}

// Spawn a new goroutine. caller is an optional string identifying the spawn site.
export function spawn(fn, caller) {
  const id = ++goroutineId;
  const g = new Goroutine(id, fn, caller || '');
  runQueue.push(g);
  scheduleRun();
  return g;
}

let scheduled = false;

function scheduleRun() {
  if (!scheduled) {
    scheduled = true;
    // Use queueMicrotask for tight scheduling, setTimeout(0) for fairness.
    queueMicrotask(tick);
  }
}

async function tick() {
  if (running) { scheduled = false; return; }
  running = true;

  while (runQueue.length > 0) {
    const g = runQueue.shift();
    if (g.done) continue;

    try {
      const result = g.fn();
      if (result instanceof Promise) {
        await result;
      }
      g.done = true;
    } catch (e) {
      g.done = true;
      g.error = e;
      // Record the error but keep the scheduler alive.
      recordError(g, e);
      // Continue processing remaining goroutines.
    }
  }

  running = false;
  scheduled = false;
}

// Yield current goroutine to let others run.
export function gosched() {
  return new Promise(resolve => {
    queueMicrotask(resolve);
  });
}

// Sleep for a duration (nanoseconds, but we convert to ms).
// ns may be BigInt (int64) or Number.
export function sleep(ns) {
  const ms = Math.max(0, Number(ns) / 1_000_000);
  return new Promise(resolve => setTimeout(resolve, ms));
}

// Run the main function and wait for all goroutines.
export async function runMain(mainFn) {
  try {
    const result = mainFn();
    if (result instanceof Promise) await result;
  } catch (e) {
    const entry = { id: 0, caller: 'main', error: e, time: Date.now() };
    _errorLog.push(entry);
    if (e && e.name === 'GoPanic') {
      console.error(`main: panic: ${e.message}`);
      if (e.stack) console.error(e.stack);
    } else {
      console.error('main: unhandled error:', e);
    }
    if (_onError) {
      try { _onError(0, 'main', e); } catch (_) {}
    }
    // Don't re-throw — let the scheduler drain remaining goroutines.
  }

  // Drain remaining goroutines.
  while (runQueue.length > 0) {
    await tick();
    // Small yield to let any newly-spawned goroutines enqueue.
    await new Promise(r => setTimeout(r, 0));
  }
}
