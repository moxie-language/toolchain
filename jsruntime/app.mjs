// Moxie jsruntime - Gio App Bridge
// Canvas creation, WebGL context setup, DOM event handling for Gio windows.

import * as webgl from './webgl.mjs';

let _canvas = null;
let _textarea = null;
let _composing = false;
let _wasm = null;     // set by loader after WASM instantiation
let _wasmReady = false; // set after _start() returns; prevents event dispatch during init
let _dpr = 1;

// Special key codes (>= 256) returned by keyCode().
const KEY_ARROW_UP    = 256;
const KEY_ARROW_DOWN  = 257;
const KEY_ARROW_LEFT  = 258;
const KEY_ARROW_RIGHT = 259;
const KEY_ESCAPE      = 260;
const KEY_ENTER       = 261;
const KEY_BACKSPACE   = 262;
const KEY_DELETE      = 263;
const KEY_HOME        = 264;
const KEY_END         = 265;
const KEY_PAGE_UP     = 266;
const KEY_PAGE_DOWN   = 267;
const KEY_TAB         = 268;
const KEY_SPACE       = 269;
const KEY_F1          = 270;
const KEY_F2          = 271;
const KEY_F3          = 272;
const KEY_F4          = 273;
const KEY_F5          = 274;
const KEY_F6          = 275;
const KEY_F7          = 276;
const KEY_F8          = 277;
const KEY_F9          = 278;
const KEY_F10         = 279;
const KEY_F11         = 280;
const KEY_F12         = 281;
const KEY_CTRL        = 282;
const KEY_SHIFT       = 283;
const KEY_ALT         = 284;
const KEY_SUPER       = 285;
const KEY_COMMAND     = 286;

function keyCode(k) {
  switch (k) {
    case 'ArrowUp':    return KEY_ARROW_UP;
    case 'ArrowDown':  return KEY_ARROW_DOWN;
    case 'ArrowLeft':  return KEY_ARROW_LEFT;
    case 'ArrowRight': return KEY_ARROW_RIGHT;
    case 'Escape':     return KEY_ESCAPE;
    case 'Enter':      return KEY_ENTER;
    case 'Backspace':  return KEY_BACKSPACE;
    case 'Delete':     return KEY_DELETE;
    case 'Home':       return KEY_HOME;
    case 'End':        return KEY_END;
    case 'PageUp':     return KEY_PAGE_UP;
    case 'PageDown':   return KEY_PAGE_DOWN;
    case 'Tab':        return KEY_TAB;
    case ' ':          return KEY_SPACE;
    case 'F1':  return KEY_F1;  case 'F2':  return KEY_F2;
    case 'F3':  return KEY_F3;  case 'F4':  return KEY_F4;
    case 'F5':  return KEY_F5;  case 'F6':  return KEY_F6;
    case 'F7':  return KEY_F7;  case 'F8':  return KEY_F8;
    case 'F9':  return KEY_F9;  case 'F10': return KEY_F10;
    case 'F11': return KEY_F11; case 'F12': return KEY_F12;
    case 'Control': return KEY_CTRL;
    case 'Shift':   return KEY_SHIFT;
    case 'Alt':     return KEY_ALT;
    case 'Meta':    return KEY_COMMAND;
    case 'OS':      return KEY_SUPER;
  }
  // Single printable character: return uppercase code point.
  if (k.length === 1) {
    const cp = k.toUpperCase().codePointAt(0);
    if (cp >= 0x20 && cp <= 0x7E) return cp;
  }
  return -1;
}

function modsFrom(e) {
  let m = 0;
  if (e.altKey)     m |= 1;
  if (e.ctrlKey)    m |= 2;
  if (e.shiftKey)   m |= 4;
  if (e.metaKey)    m |= 8;
  return m;
}

function mem32() {
  return new Int32Array(_wasm.exports.memory.buffer);
}

export function SetWASM(instance) {
  _wasm = instance;
}

export function SetWASMReady() {
  _wasmReady = true;
}

// --- Bridge functions called from Moxie ---

export function CreateFullscreenCanvas() {
  const canvas = document.createElement('canvas');
  canvas.style.cssText = 'position:fixed;left:0;top:0;width:100%;height:100%;display:block;';
  document.body.style.cssText = 'margin:0;padding:0;overflow:hidden;';
  document.body.appendChild(canvas);
  _canvas = canvas;

  // Hidden textarea: keyboard focus target for text input and IME composition.
  const tarea = document.createElement('textarea');
  const ts = tarea.style;
  ts.position = 'absolute';
  ts.left = '-9999px';
  ts.top = '0';
  ts.width = '1px';
  ts.height = '1px';
  ts.opacity = '0';
  ts.border = '0';
  ts.padding = '0';
  ts.overflow = 'hidden';
  ts.resize = 'none';
  tarea.setAttribute('autocomplete', 'off');
  tarea.setAttribute('autocorrect', 'off');
  tarea.setAttribute('autocapitalize', 'off');
  tarea.spellcheck = false;
  tarea.rows = 1;
  document.body.appendChild(tarea);
  _textarea = tarea;
}

export function CreateWebGLContext() {
  if (!_canvas) return 0;
  const gl = _canvas.getContext('webgl2', {
    alpha: true,
    depth: true,
    stencil: true,
    antialias: false,
    premultipliedAlpha: true,
    preserveDrawingBuffer: false,
  });
  if (!gl) return 0;
  // Enable float FBO rendering for stencil-and-cover path rendering.
  // Without this, negative coverage values (for left-moving bezier segments)
  // are clamped to 0, causing D-shaped rendering artifacts on circles/rrects.
  gl.getExtension('EXT_color_buffer_float');
  gl.getExtension('EXT_color_buffer_half_float');
  return webgl.RegisterContext(gl);
}

export function GetDevicePixelRatio() {
  _dpr = window.devicePixelRatio || 1;
  return Math.fround(_dpr);
}

export function GetCanvasCSSSize(wPtr, hPtr) {
  if (!_canvas || !_wasm) return;
  const rect = _canvas.getBoundingClientRect();
  // Fall back to window size if layout hasn't completed yet.
  const w = rect.width > 1 ? rect.width : window.innerWidth;
  const h = rect.height > 1 ? rect.height : window.innerHeight;
  const m = mem32();
  m[(wPtr >>> 0) >> 2] = Math.round(w);
  m[(hPtr >>> 0) >> 2] = Math.round(h);
}

export function SetCanvasBacking(w, h) {
  if (!_canvas) return;
  _canvas.width = w;
  _canvas.height = h;
}

export function RequestRAF() {
  requestAnimationFrame(() => {
    if (_wasm && _wasm.exports.__gio_raf) {
      _wasm.exports.__gio_raf();
    }
  });
}

export function SetCursor(ptr, len) {
  if (!_canvas || !_wasm) return;
  const bytes = new Uint8Array(_wasm.exports.memory.buffer, ptr >>> 0, len);
  _canvas.style.cursor = new TextDecoder().decode(bytes);
}

export function SetTitle(ptr, len) {
  if (!_wasm) return;
  const bytes = new Uint8Array(_wasm.exports.memory.buffer, ptr >>> 0, len);
  document.title = new TextDecoder().decode(bytes);
}

export function SetFullscreen(full) {
  if (full) {
    if (document.documentElement.requestFullscreen) {
      document.documentElement.requestFullscreen();
    }
  } else {
    if (document.exitFullscreen) {
      document.exitFullscreen();
    }
  }
}

export function RegisterPointerEvents() {
  if (!_canvas) return;
  // pointer kind constants: 0=Move, 1=Press, 2=Release, 3=Scroll, 4=Cancel
  _canvas.addEventListener('mousemove', e => {
    e.preventDefault();
    dispatchPointer(0, e, 0, 0);
  });
  _canvas.addEventListener('mousedown', e => {
    e.preventDefault();
    dispatchPointer(1, e, 0, 0);
    // Focus textarea on click so keyboard events are captured.
    if (_textarea) _textarea.focus({ preventScroll: true });
  });
  _canvas.addEventListener('mouseup', e => {
    e.preventDefault();
    dispatchPointer(2, e, 0, 0);
  });
  _canvas.addEventListener('wheel', e => {
    e.preventDefault();
    let dx = e.deltaX, dy = e.deltaY;
    if (e.shiftKey) { const tmp = dx; dx = dy; dy = tmp; }
    if (e.deltaMode === 1) { dx *= 10; dy *= 10; }
    if (e.deltaMode === 2) { dx *= 120; dy *= 120; }
    const dpr = window.devicePixelRatio || 1;
    dispatchPointer(3, e, dx * dpr, dy * dpr);
  }, { passive: false });
  // Prevent right-click context menu over canvas.
  _canvas.addEventListener('contextmenu', e => e.preventDefault());
  // touch events
  _canvas.addEventListener('touchstart', e => {
    e.preventDefault();
    const rect = _canvas.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    for (let i = 0; i < e.changedTouches.length; i++) {
      const t = e.changedTouches[i];
      dispatchTouchPoint(1, t, rect, dpr, modsFrom(e));
    }
    if (_textarea) _textarea.focus({ preventScroll: true });
  }, { passive: false });
  _canvas.addEventListener('touchend', e => {
    e.preventDefault();
    const rect = _canvas.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    for (let i = 0; i < e.changedTouches.length; i++) {
      const t = e.changedTouches[i];
      dispatchTouchPoint(2, t, rect, dpr, modsFrom(e));
    }
  }, { passive: false });
  _canvas.addEventListener('touchmove', e => {
    e.preventDefault();
    const rect = _canvas.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    for (let i = 0; i < e.changedTouches.length; i++) {
      const t = e.changedTouches[i];
      dispatchTouchPoint(0, t, rect, dpr, modsFrom(e));
    }
  }, { passive: false });
  _canvas.addEventListener('touchcancel', e => {
    if (_wasm && _wasm.exports.__gio_pointer) {
      _wasm.exports.__gio_pointer(4, 0, 0, 0, 0, 0, 0); // Cancel
    }
  });
}

function dispatchPointer(kind, e, dx, dy) {
  if (!_wasm || !_wasmReady || !_wasm.exports.__gio_pointer) return;
  const rect = _canvas.getBoundingClientRect();
  const dpr = window.devicePixelRatio || 1;
  const x = (e.clientX - rect.left) * dpr;
  const y = (e.clientY - rect.top) * dpr;
  const btns = e.buttons;
  const mods = modsFrom(e);
  _wasm.exports.__gio_pointer(kind, Math.fround(x), Math.fround(y), Math.fround(dx), Math.fround(dy), btns, mods);
}

function dispatchTouchPoint(kind, touch, rect, dpr, mods) {
  if (!_wasm || !_wasm.exports.__gio_pointer) return;
  const x = (touch.clientX - rect.left) * dpr;
  const y = (touch.clientY - rect.top) * dpr;
  const id = touch.identifier & 0xff; // pack touch id in high bits of buttons
  _wasm.exports.__gio_pointer(kind, Math.fround(x), Math.fround(y), 0, 0, id << 8, mods);
}

export function RegisterKeyEvents() {
  document.addEventListener('keydown', e => {
    // Ctrl+V / Cmd+V: intercept for clipboard paste.
    if ((e.ctrlKey || e.metaKey) && e.key === 'v') {
      e.preventDefault();
      if (navigator.clipboard && navigator.clipboard.readText) {
        navigator.clipboard.readText().then(text => {
          dispatchPaste(text);
        }).catch(() => {});
      }
      // Still dispatch the key event so Moxie knows a paste was requested.
    }
    dispatchKey(e, 1);
  });
  document.addEventListener('keyup', e => dispatchKey(e, 0));
}

function dispatchKey(e, press) {
  if (!_wasm || !_wasmReady || !_wasm.exports.__gio_key) return;
  const code = keyCode(e.key);
  if (code < 0) return;
  const mods = modsFrom(e);
  // Prevent browser default for function/navigation keys on press.
  if (press && code >= KEY_ARROW_UP) {
    e.preventDefault();
  }
  _wasm.exports.__gio_key(code, mods, press);
}

export function RegisterResizeEvents() {
  const vp = window.visualViewport || window;
  vp.addEventListener('resize', () => {
    if (_wasm && _wasm.exports.__gio_resize) {
      _wasm.exports.__gio_resize();
    }
  });
}

// --- Text input (Step 1) ---

export function RegisterTextEvents() {
  if (!_textarea) return;
  _textarea.addEventListener('compositionstart', () => {
    _composing = true;
  });
  _textarea.addEventListener('compositionend', e => {
    _composing = false;
    if (e.data) dispatchText(e.data);
    _textarea.value = '';
  });
  _textarea.addEventListener('input', e => {
    if (_composing) return; // skip intermediate IME state
    const data = e.data;
    if (data) {
      dispatchText(data);
      _textarea.value = '';
    }
  });
  _textarea.addEventListener('focus', () => {
    if (_wasm && _wasmReady && _wasm.exports.__gio_focus) {
      _wasm.exports.__gio_focus(1);
    }
  });
  _textarea.addEventListener('blur', () => {
    if (_wasm && _wasmReady && _wasm.exports.__gio_focus) {
      _wasm.exports.__gio_focus(0);
    }
  });
}

function dispatchText(str) {
  if (!_wasm || !_wasmReady || !_wasm.exports.__gio_text) return;
  for (const ch of str) {
    const cp = ch.codePointAt(0);
    if (cp !== undefined) _wasm.exports.__gio_text(cp);
  }
}

function dispatchPaste(str) {
  if (!_wasm || !_wasmReady || !_wasm.exports.__gio_paste) return;
  for (const ch of str) {
    const cp = ch.codePointAt(0);
    if (cp !== undefined) _wasm.exports.__gio_paste(cp);
  }
}

export function FocusTextArea() {
  if (_textarea) _textarea.focus({ preventScroll: true });
}

export function BlurTextArea() {
  if (_textarea) _textarea.blur();
}

// --- Clipboard (Step 3) ---

export function WriteClipboard(ptr, len) {
  if (!_wasm || !navigator.clipboard) return;
  const bytes = new Uint8Array(_wasm.exports.memory.buffer, ptr >>> 0, len);
  const text = new TextDecoder().decode(bytes);
  navigator.clipboard.writeText(text).catch(() => {});
}

export function ReadClipboard() {
  if (!navigator.clipboard) return;
  navigator.clipboard.readText().then(text => {
    dispatchPaste(text);
  }).catch(() => {});
}

// --- WebGL context loss (Step 4) ---

export function RegisterContextLossEvents() {
  if (!_canvas) return;
  _canvas.addEventListener('webglcontextlost', e => {
    e.preventDefault();
    if (_wasm && _wasmReady && _wasm.exports.__gio_context_loss) {
      _wasm.exports.__gio_context_loss(1);
    }
  });
  _canvas.addEventListener('webglcontextrestored', () => {
    if (_wasm && _wasmReady && _wasm.exports.__gio_context_loss) {
      _wasm.exports.__gio_context_loss(0);
    }
  });
}
