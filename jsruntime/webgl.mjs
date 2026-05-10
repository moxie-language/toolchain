// Moxie jsruntime - WebGL2 Bridge
// Handle-based shim for WebGL2 contexts and objects.
// Functions read/write WASM memory directly via globalThis.__moxie_wasm_memory.

const _contexts = new Map();
const _buffers = new Map();
const _textures = new Map();
const _framebuffers = new Map();
const _renderbuffers = new Map();
const _shaders = new Map();
const _programs = new Map();
const _uniformLocs = new Map();
const _vaos = new Map();

let _nextCtx = 1;
let _nextBuf = 1;
let _nextTex = 1;
let _nextFbo = 1;
let _nextRbo = 1;
let _nextShader = 1;
let _nextProg = 1;
let _nextUloc = 1;
let _nextVao = 1;

function _mem() {
  return globalThis.__moxie_wasm_memory;
}

function _readBytes(ptr, len) {
  if (!ptr || len <= 0) return null;
  return new Uint8Array(_mem().buffer, ptr, len);
}

function _readStr(ptr, len) {
  if (!ptr || len <= 0) return '';
  return new TextDecoder().decode(new Uint8Array(_mem().buffer, ptr, len));
}

function _writeBytes(ptr, data) {
  new Uint8Array(_mem().buffer, ptr, data.length).set(data);
}

// --- Context ---

// RegisterContext registers an existing WebGL2 context into the handle system.
// Called by app.mjs after creating the context from a canvas element.
export function RegisterContext(gl) {
  if (!gl) return 0;
  const id = _nextCtx++;
  _contexts.set(id, gl);
  return id;
}

export function CreateContext(canvasId) {
  const canvas = document.getElementById(canvasId) || document.querySelector('canvas');
  if (!canvas) return 0;
  const gl = canvas.getContext('webgl2', {
    alpha: true,
    depth: true,
    stencil: true,
    antialias: false,
    premultipliedAlpha: true,
    preserveDrawingBuffer: false,
  });
  if (!gl) return 0;
  const id = _nextCtx++;
  _contexts.set(id, gl);
  return id;
}

export function CreateContextFromHandle(elHandle) {
  // For use with DOM handle system - caller passes DOM element handle.
  // The canvas element must be retrieved from the DOM module's handle map.
  // Fallback: if globalThis.__moxie_dom_elements exists, use it.
  const elements = globalThis.__moxie_dom_elements;
  if (!elements) return 0;
  const canvas = elements.get(elHandle);
  if (!canvas || canvas.tagName !== 'CANVAS') return 0;
  const gl = canvas.getContext('webgl2', {
    alpha: true,
    depth: true,
    stencil: true,
    antialias: false,
    premultipliedAlpha: true,
    preserveDrawingBuffer: false,
  });
  if (!gl) return 0;
  const id = _nextCtx++;
  _contexts.set(id, gl);
  return id;
}

export function DeleteContext(ctx) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  gl.getExtension('WEBGL_lose_context')?.loseContext();
  _contexts.delete(ctx);
}

export function IsWebGL2(ctx) {
  const gl = _contexts.get(ctx);
  return gl instanceof WebGL2RenderingContext ? 1 : 0;
}

// --- State ---

export function Enable(ctx, cap) {
  _contexts.get(ctx)?.enable(cap);
}

export function Disable(ctx, cap) {
  _contexts.get(ctx)?.disable(cap);
}

export function Viewport(ctx, x, y, w, h) {
  _contexts.get(ctx)?.viewport(x, y, w, h);
}

export function Scissor(ctx, x, y, w, h) {
  _contexts.get(ctx)?.scissor(x, y, w, h);
}

export function BlendFunc(ctx, src, dst) {
  _contexts.get(ctx)?.blendFunc(src, dst);
}

export function BlendFuncSeparate(ctx, srcRGB, dstRGB, srcA, dstA) {
  _contexts.get(ctx)?.blendFuncSeparate(srcRGB, dstRGB, srcA, dstA);
}

export function DepthFunc(ctx, func) {
  _contexts.get(ctx)?.depthFunc(func);
}

export function DepthMask(ctx, flag) {
  _contexts.get(ctx)?.depthMask(!!flag);
}

export function ColorMask(ctx, r, g, b, a) {
  _contexts.get(ctx)?.colorMask(!!r, !!g, !!b, !!a);
}

export function StencilFunc(ctx, func, ref, mask) {
  _contexts.get(ctx)?.stencilFunc(func, ref, mask);
}

export function StencilOp(ctx, sfail, dpfail, dppass) {
  _contexts.get(ctx)?.stencilOp(sfail, dpfail, dppass);
}

export function ClearColor(ctx, r, g, b, a) {
  _contexts.get(ctx)?.clearColor(r, g, b, a);
}

export function ClearDepth(ctx, d) {
  _contexts.get(ctx)?.clearDepth(d);
}

export function ClearStencil(ctx, s) {
  _contexts.get(ctx)?.clearStencil(s);
}

export function Clear(ctx, mask) {
  _contexts.get(ctx)?.clear(mask);
}

export function Flush(ctx) {
  _contexts.get(ctx)?.flush();
}

export function GetError(ctx) {
  const gl = _contexts.get(ctx);
  return gl ? gl.getError() : 0;
}

export function PixelStorei(ctx, pname, param) {
  _contexts.get(ctx)?.pixelStorei(pname, param);
}

export function ReadPixels(ctx, x, y, w, h, format, type, bufPtr, bufLen) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  const dst = new Uint8Array(_mem().buffer, bufPtr, bufLen);
  gl.readPixels(x, y, w, h, format, type, dst);
}

// --- Buffers ---

export function CreateBuffer(ctx) {
  const gl = _contexts.get(ctx);
  if (!gl) return 0;
  const buf = gl.createBuffer();
  if (!buf) return 0;
  const id = _nextBuf++;
  _buffers.set(id, buf);
  return id;
}

export function DeleteBuffer(ctx, buf) {
  const gl = _contexts.get(ctx);
  const obj = _buffers.get(buf);
  if (gl && obj) gl.deleteBuffer(obj);
  _buffers.delete(buf);
}

export function BindBuffer(ctx, target, buf) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  gl.bindBuffer(target, buf ? _buffers.get(buf) || null : null);
}

export function BufferData(ctx, target, dataPtr, dataLen, usage) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  const data = _readBytes(dataPtr, dataLen);
  if (data) {
    gl.bufferData(target, data, usage);
  } else {
    gl.bufferData(target, dataLen, usage);
  }
}

export function BufferSubData(ctx, target, offset, dataPtr, dataLen) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  const data = _readBytes(dataPtr, dataLen);
  if (data) gl.bufferSubData(target, offset, data);
}

// --- Textures ---

export function CreateTexture(ctx) {
  const gl = _contexts.get(ctx);
  if (!gl) return 0;
  const tex = gl.createTexture();
  if (!tex) return 0;
  const id = _nextTex++;
  _textures.set(id, tex);
  return id;
}

export function DeleteTexture(ctx, tex) {
  const gl = _contexts.get(ctx);
  const obj = _textures.get(tex);
  if (gl && obj) gl.deleteTexture(obj);
  _textures.delete(tex);
}

export function BindTexture(ctx, target, tex) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  gl.bindTexture(target, tex ? _textures.get(tex) || null : null);
}

export function ActiveTexture(ctx, unit) {
  _contexts.get(ctx)?.activeTexture(unit);
}

export function TexParameteri(ctx, target, pname, param) {
  _contexts.get(ctx)?.texParameteri(target, pname, param);
}

export function TexImage2D(ctx, target, level, internalformat, width, height, border, format, type, dataPtr, dataLen) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  const data = _readBytes(dataPtr, dataLen);
  gl.texImage2D(target, level, internalformat, width, height, border, format, type, data);
}

export function TexSubImage2D(ctx, target, level, xoff, yoff, width, height, format, type, dataPtr, dataLen) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  const data = _readBytes(dataPtr, dataLen);
  if (data) gl.texSubImage2D(target, level, xoff, yoff, width, height, format, type, data);
}

export function GenerateMipmap(ctx, target) {
  _contexts.get(ctx)?.generateMipmap(target);
}

// --- Framebuffers ---

export function CreateFramebuffer(ctx) {
  const gl = _contexts.get(ctx);
  if (!gl) return 0;
  const fbo = gl.createFramebuffer();
  if (!fbo) return 0;
  const id = _nextFbo++;
  _framebuffers.set(id, fbo);
  return id;
}

export function DeleteFramebuffer(ctx, fbo) {
  const gl = _contexts.get(ctx);
  const obj = _framebuffers.get(fbo);
  if (gl && obj) gl.deleteFramebuffer(obj);
  _framebuffers.delete(fbo);
}

export function BindFramebuffer(ctx, target, fbo) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  gl.bindFramebuffer(target, fbo ? _framebuffers.get(fbo) || null : null);
}

export function FramebufferTexture2D(ctx, target, attachment, textarget, tex, level) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  const obj = tex ? _textures.get(tex) || null : null;
  gl.framebufferTexture2D(target, attachment, textarget, obj, level);
}

export function CheckFramebufferStatus(ctx, target) {
  const gl = _contexts.get(ctx);
  return gl ? gl.checkFramebufferStatus(target) : 0;
}

// --- Renderbuffers ---

export function CreateRenderbuffer(ctx) {
  const gl = _contexts.get(ctx);
  if (!gl) return 0;
  const rbo = gl.createRenderbuffer();
  if (!rbo) return 0;
  const id = _nextRbo++;
  _renderbuffers.set(id, rbo);
  return id;
}

export function DeleteRenderbuffer(ctx, rbo) {
  const gl = _contexts.get(ctx);
  const obj = _renderbuffers.get(rbo);
  if (gl && obj) gl.deleteRenderbuffer(obj);
  _renderbuffers.delete(rbo);
}

export function BindRenderbuffer(ctx, target, rbo) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  gl.bindRenderbuffer(target, rbo ? _renderbuffers.get(rbo) || null : null);
}

export function RenderbufferStorage(ctx, target, internalformat, width, height) {
  _contexts.get(ctx)?.renderbufferStorage(target, internalformat, width, height);
}

export function FramebufferRenderbuffer(ctx, target, attachment, renderbuffertarget, rbo) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  const obj = rbo ? _renderbuffers.get(rbo) || null : null;
  gl.framebufferRenderbuffer(target, attachment, renderbuffertarget, obj);
}

// --- Shaders ---

export function CreateShader(ctx, type) {
  const gl = _contexts.get(ctx);
  if (!gl) return 0;
  const shader = gl.createShader(type);
  if (!shader) return 0;
  const id = _nextShader++;
  _shaders.set(id, shader);
  return id;
}

export function DeleteShader(ctx, shader) {
  const gl = _contexts.get(ctx);
  const obj = _shaders.get(shader);
  if (gl && obj) gl.deleteShader(obj);
  _shaders.delete(shader);
}

export function ShaderSource(ctx, shader, srcPtr, srcLen) {
  const gl = _contexts.get(ctx);
  const obj = _shaders.get(shader);
  if (!gl || !obj) return;
  gl.shaderSource(obj, _readStr(srcPtr, srcLen));
}

export function CompileShader(ctx, shader) {
  const gl = _contexts.get(ctx);
  const obj = _shaders.get(shader);
  if (gl && obj) gl.compileShader(obj);
}

export function GetShaderParameter(ctx, shader, pname) {
  const gl = _contexts.get(ctx);
  const obj = _shaders.get(shader);
  if (!gl || !obj) return 0;
  const v = gl.getShaderParameter(obj, pname);
  return typeof v === 'boolean' ? (v ? 1 : 0) : (v | 0);
}

export function GetShaderInfoLog(ctx, shader, bufPtr, bufLen) {
  const gl = _contexts.get(ctx);
  const obj = _shaders.get(shader);
  if (!gl || !obj) return 0;
  const log = gl.getShaderInfoLog(obj) || '';
  const encoded = new TextEncoder().encode(log);
  const n = Math.min(encoded.length, bufLen);
  if (n > 0) _writeBytes(bufPtr, encoded.subarray(0, n));
  return encoded.length;
}

// --- Programs ---

export function CreateProgram(ctx) {
  const gl = _contexts.get(ctx);
  if (!gl) return 0;
  const prog = gl.createProgram();
  if (!prog) return 0;
  const id = _nextProg++;
  _programs.set(id, prog);
  return id;
}

export function DeleteProgram(ctx, prog) {
  const gl = _contexts.get(ctx);
  const obj = _programs.get(prog);
  if (gl && obj) gl.deleteProgram(obj);
  _programs.delete(prog);
}

export function AttachShader(ctx, prog, shader) {
  const gl = _contexts.get(ctx);
  const p = _programs.get(prog);
  const s = _shaders.get(shader);
  if (gl && p && s) gl.attachShader(p, s);
}

export function LinkProgram(ctx, prog) {
  const gl = _contexts.get(ctx);
  const obj = _programs.get(prog);
  if (gl && obj) gl.linkProgram(obj);
}

export function UseProgram(ctx, prog) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  gl.useProgram(prog ? _programs.get(prog) || null : null);
}

export function GetProgramParameter(ctx, prog, pname) {
  const gl = _contexts.get(ctx);
  const obj = _programs.get(prog);
  if (!gl || !obj) return 0;
  const v = gl.getProgramParameter(obj, pname);
  return typeof v === 'boolean' ? (v ? 1 : 0) : (v | 0);
}

export function GetProgramInfoLog(ctx, prog, bufPtr, bufLen) {
  const gl = _contexts.get(ctx);
  const obj = _programs.get(prog);
  if (!gl || !obj) return 0;
  const log = gl.getProgramInfoLog(obj) || '';
  const encoded = new TextEncoder().encode(log);
  const n = Math.min(encoded.length, bufLen);
  if (n > 0) _writeBytes(bufPtr, encoded.subarray(0, n));
  return encoded.length;
}

export function BindAttribLocation(ctx, prog, index, namePtr, nameLen) {
  const gl = _contexts.get(ctx);
  const obj = _programs.get(prog);
  if (!gl || !obj) return;
  gl.bindAttribLocation(obj, index, _readStr(namePtr, nameLen));
}

export function GetUniformLocation(ctx, prog, namePtr, nameLen) {
  const gl = _contexts.get(ctx);
  const obj = _programs.get(prog);
  if (!gl || !obj) return 0;
  const loc = gl.getUniformLocation(obj, _readStr(namePtr, nameLen));
  if (!loc) return 0;
  const id = _nextUloc++;
  _uniformLocs.set(id, loc);
  return id;
}

// --- Uniforms ---

export function Uniform1f(ctx, loc, v0) {
  const gl = _contexts.get(ctx);
  const u = _uniformLocs.get(loc);
  if (gl && u) gl.uniform1f(u, v0);
}

export function Uniform2f(ctx, loc, v0, v1) {
  const gl = _contexts.get(ctx);
  const u = _uniformLocs.get(loc);
  if (gl && u) gl.uniform2f(u, v0, v1);
}

export function Uniform4f(ctx, loc, v0, v1, v2, v3) {
  const gl = _contexts.get(ctx);
  const u = _uniformLocs.get(loc);
  if (gl && u) gl.uniform4f(u, v0, v1, v2, v3);
}

export function Uniform1i(ctx, loc, v0) {
  const gl = _contexts.get(ctx);
  const u = _uniformLocs.get(loc);
  if (gl && u) gl.uniform1i(u, v0);
}

// --- Vertex Attributes ---

export function EnableVertexAttribArray(ctx, index) {
  _contexts.get(ctx)?.enableVertexAttribArray(index);
}

export function DisableVertexAttribArray(ctx, index) {
  _contexts.get(ctx)?.disableVertexAttribArray(index);
}

export function VertexAttribPointer(ctx, index, size, type, normalized, stride, offset) {
  _contexts.get(ctx)?.vertexAttribPointer(index, size, type, !!normalized, stride, offset);
}

// --- VAO (WebGL2) ---

export function CreateVertexArray(ctx) {
  const gl = _contexts.get(ctx);
  if (!gl) return 0;
  const vao = gl.createVertexArray();
  if (!vao) return 0;
  const id = _nextVao++;
  _vaos.set(id, vao);
  return id;
}

export function DeleteVertexArray(ctx, vao) {
  const gl = _contexts.get(ctx);
  const obj = _vaos.get(vao);
  if (gl && obj) gl.deleteVertexArray(obj);
  _vaos.delete(vao);
}

export function BindVertexArray(ctx, vao) {
  const gl = _contexts.get(ctx);
  if (!gl) return;
  gl.bindVertexArray(vao ? _vaos.get(vao) || null : null);
}

// --- Blending and misc state ---

export function BlendEquation(ctx, mode) {
  _contexts.get(ctx)?.blendEquation(mode);
}

// --- Mipmap textures ---

export function TexStorage2D(ctx, target, levels, internalformat, width, height) {
  _contexts.get(ctx)?.texStorage2D(target, levels, internalformat, width, height);
}

// --- Framebuffer ops ---

export function CopyTexSubImage2D(ctx, target, level, xoff, yoff, x, y, w, h) {
  _contexts.get(ctx)?.copyTexSubImage2D(target, level, xoff, yoff, x, y, w, h);
}

export function InvalidateFramebuffer(ctx, target, attachment) {
  const gl = _contexts.get(ctx);
  if (!gl || !gl.invalidateFramebuffer) return;
  gl.invalidateFramebuffer(target, [attachment]);
}

// --- Queries ---

export function GetInteger(ctx, pname) {
  const gl = _contexts.get(ctx);
  return gl ? (gl.getParameter(pname) | 0) : 0;
}

export function IsEnabled(ctx, cap) {
  const gl = _contexts.get(ctx);
  return gl && gl.isEnabled(cap) ? 1 : 0;
}

// --- Finish ---

export function Finish(ctx) {
  _contexts.get(ctx)?.finish();
}

// --- Extra uniforms ---

export function Uniform3f(ctx, loc, v0, v1, v2) {
  const gl = _contexts.get(ctx);
  const u = _uniformLocs.get(loc);
  if (gl && u) gl.uniform3f(u, v0, v1, v2);
}

// --- Drawing ---

export function DrawArrays(ctx, mode, first, count) {
  _contexts.get(ctx)?.drawArrays(mode, first, count);
}

export function DrawElements(ctx, mode, count, type, offset) {
  _contexts.get(ctx)?.drawElements(mode, count, type, offset);
}
