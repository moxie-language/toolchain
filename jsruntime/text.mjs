// Moxie jsruntime - Browser Text Rendering Bridge
// Renders text using browser's native canvas 2D API.
// Uses a shared hidden canvas for synchronous measurement and rendering.

let _canvas = null;
let _ctx = null;
let _wasm = null;

export function SetWASM(instance) {
  _wasm = instance;
}

function ensureCanvas(w, h) {
  if (!_canvas) {
    _canvas = document.createElement('canvas');
    _canvas.style.display = 'none';
    document.body.appendChild(_canvas);
    _ctx = _canvas.getContext('2d');
  }
  if (_canvas.width < w) _canvas.width = w;
  if (_canvas.height < h) _canvas.height = h;
}

function readStr(ptr, len) {
  const uptr = ptr >>> 0;
  if (!uptr || len <= 0 || !_wasm) return '';
  return new TextDecoder().decode(new Uint8Array(_wasm.exports.memory.buffer, uptr, len));
}

function setI32(ptr, val) {
  if (!_wasm) return;
  const view = new DataView(_wasm.exports.memory.buffer);
  view.setInt32(ptr >>> 0, val, true);
}

function fontCSS(family, size) {
  // family format: "weight [italic] font-name" e.g. "700 sans-serif"
  // CSS font shorthand: weight [style] size family
  const parts = (family || 'sans-serif').split(' ');
  const weight = parts[0];
  const rest = parts.slice(1).join(' ') || 'sans-serif';
  return `${weight} ${size}px ${rest}`;
}

// Measure returns pixel width and height of the given text string.
// Writes two int32 values at wPtr and hPtr (physical pixels).
export function Measure(fontPtr, fontLen, size, textPtr, textLen, wPtr, hPtr) {
  const family = readStr(fontPtr, fontLen);
  const text = readStr(textPtr, textLen);
  ensureCanvas(1, 1);
  _ctx.font = fontCSS(family, size);
  const m = _ctx.measureText(text);
  // Add +4 to match Render's padding - WASM allocates based on Measure's output.
  const w = Math.ceil(m.width) + 4;
  // Use actual bounding box if available (modern browsers), else estimate.
  let ascent = size;
  let descent = size * 0.25;
  if (m.actualBoundingBoxAscent !== undefined) {
    ascent = m.actualBoundingBoxAscent;
    descent = m.actualBoundingBoxDescent;
  }
  const h = Math.ceil(ascent + descent + 2); // +2px padding
  setI32(wPtr, w);
  setI32(hPtr, h);
}

// Render renders text into WASM memory as RGBA pixels.
// Clears the canvas, draws white text, reads back pixels.
// maxW: canvas width (0 = auto from text width).
// dataPtr: WASM memory pointer, dataCap: buffer capacity.
// Returns actual bytes written (w * h * 4), or 0 if buffer too small.
export function Render(fontPtr, fontLen, size, textPtr, textLen, maxW, dataPtr, dataCap) {
  const family = readStr(fontPtr, fontLen);
  const text = readStr(textPtr, textLen);
  _ctx.font = fontCSS(family, size);

  // Measure first.
  const m = _ctx.measureText(text);
  const textW = Math.ceil(m.width);
  let ascent = size;
  let descent = size * 0.25;
  if (m.actualBoundingBoxAscent !== undefined) {
    ascent = m.actualBoundingBoxAscent;
    descent = m.actualBoundingBoxDescent;
  }
  const h = Math.ceil(ascent + descent + 2);
  const w = maxW > 0 ? Math.min(maxW, textW + 4) : textW + 4;

  if (w * h * 4 > dataCap) return 0; // buffer too small

  ensureCanvas(w, h);
  _ctx.font = fontCSS(family, size); // re-set after canvas resize
  _ctx.clearRect(0, 0, w, h);
  _ctx.fillStyle = 'black';
  _ctx.textBaseline = 'top';
  _ctx.fillText(text, 1, 1); // 1px inset for sub-pixel safety

  const imageData = _ctx.getImageData(0, 0, w, h);
  const bytes = imageData.data; // Uint8ClampedArray, length = w*h*4
  const n = w * h * 4;
  new Uint8Array(_wasm.exports.memory.buffer, dataPtr >>> 0, n).set(bytes.subarray(0, n));
  return n;
}
