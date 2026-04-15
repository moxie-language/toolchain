// MoxieJS Runtime — Node.js stdio bridge.
// Implements smesh.lol/web/common/jsbridge/node — line-oriented stdin/stdout.
// Only meaningful under Node; in browser/SW targets these are no-ops.

// Convert a Moxie string (which may be a JS string or a {$array,$offset,$length}
// slice shape depending on how the binding crosses the boundary) to a plain JS
// string. types.mjs intentionally does not export this; everything we need is
// visible in the slice shape itself.
function _stringify(s) {
  if (typeof s === 'string') return s;
  if (s && typeof s.$length === 'number' && s.$array) {
    let out = '';
    for (let i = 0; i < s.$length; i++) out += String.fromCharCode(s.$array[s.$offset + i]);
    return out;
  }
  return String(s);
}

let _lineHandler = null;
let _closeHandler = null;
let _wired = false;

function _wireStdinOnce() {
  if (_wired) return;
  _wired = true;

  // Node-only.
  if (typeof process === 'undefined' || !process.stdin) return;

  let buf = '';
  process.stdin.setEncoding('utf8');
  process.stdin.on('data', (chunk) => {
    buf += chunk;
    let nl;
    while ((nl = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, nl);
      buf = buf.slice(nl + 1);
      if (_lineHandler) {
        try { _lineHandler(line); } catch (e) { console.error('OnLine handler threw:', e); }
      }
    }
  });
  process.stdin.on('end', () => {
    // Flush any trailing unterminated line.
    if (buf.length > 0 && _lineHandler) {
      try { _lineHandler(buf); } catch (e) { console.error('OnLine handler threw:', e); }
      buf = '';
    }
    if (_closeHandler) {
      try { _closeHandler(); } catch (e) { console.error('OnClose handler threw:', e); }
    }
  });
}

export function OnLine(fn) {
  _lineHandler = fn;
  _wireStdinOnce();
}

export function OnClose(fn) {
  _closeHandler = fn;
  _wireStdinOnce();
}

export function WriteLine(s) {
  const str = _stringify(s);
  if (typeof process !== 'undefined' && process.stdout) {
    process.stdout.write(str + '\n');
  } else {
    console.log(str);
  }
}

export function WriteErr(s) {
  const str = _stringify(s);
  if (typeof process !== 'undefined' && process.stderr) {
    process.stderr.write(str + '\n');
  } else {
    console.error(str);
  }
}

export function Exit(code) {
  if (typeof process !== 'undefined' && typeof process.exit === 'function') {
    process.exit(typeof code === 'number' ? code : 0);
  }
}

export function NowSeconds() {
  // Moxie side declares int64, so the boundary must emit BigInt to avoid
  // "Cannot mix BigInt and other types" at call sites that do int64 math.
  return BigInt(Math.floor(Date.now() / 1000));
}
